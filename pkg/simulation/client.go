package simulation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/commserver"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"
	pion "github.com/pion/webrtc/v2"
)

const (
	writeWait = 60 * time.Second
)

type peerData struct {
	Alias            uint64
	AvailableServers []uint64
}

// Config is the client config
type Config struct {
	ICEServers        []pion.ICEServer
	Auth              authentication.ClientAuthenticator
	OnMessageReceived func(reliable bool, msgType protocol.MessageType, raw []byte)
	CoordinatorURL    string
}

// Client represents a peer with role CLIENT
type Client struct {
	iceServers        []pion.ICEServer
	onMessageReceived func(reliable bool, msgType protocol.MessageType, raw []byte)

	SendReliable        chan []byte
	SendUnreliable      chan []byte
	StopReliableQueue   chan bool
	StopUnreliableQueue chan bool
	PeerData            chan peerData

	coordinatorURL        string
	coordinator           *websocket.Conn
	conn                  *pion.PeerConnection
	authMessage           chan []byte
	coordinatorWriteQueue chan []byte
}

// MakeClient creates a new client
func MakeClient(config *Config) *Client {
	url, err := config.Auth.GenerateClientConnectURL(config.CoordinatorURL)
	if err != nil {
		log.Fatal(err)
	}

	c := &Client{
		iceServers:            config.ICEServers,
		onMessageReceived:     config.OnMessageReceived,
		coordinatorURL:        url,
		authMessage:           make(chan []byte),
		SendReliable:          make(chan []byte, 256),
		SendUnreliable:        make(chan []byte, 256),
		StopReliableQueue:     make(chan bool),
		StopUnreliableQueue:   make(chan bool),
		PeerData:              make(chan peerData),
		coordinatorWriteQueue: make(chan []byte, 256),
	}

	return c
}

// SendTopicSubscriptionMessage sends a topic subscription message to the comm server
func (client *Client) SendTopicSubscriptionMessage(topics map[string]bool) error {
	buffer := bytes.Buffer{}

	i := 0
	last := len(topics) - 1
	for topic := range topics {
		if _, err := buffer.WriteString(topic); err != nil {
			return err
		}

		if i != last {
			if _, err := buffer.WriteString(" "); err != nil {
				return err
			}
		}
		i++
	}

	gzip := commserver.GzipCompression{}
	encodedTopics, err := gzip.Zip(buffer.Bytes())
	if err != nil {
		return err
	}

	message := &protocol.SubscriptionMessage{
		Type:   protocol.MessageType_SUBSCRIPTION,
		Format: protocol.Format_GZIP,
		Topics: encodedTopics,
	}

	bytes, err := proto.Marshal(message)
	if err != nil {
		return err
	}

	client.SendReliable <- bytes
	return nil
}

// Connect connect to specified server
func (client *Client) Connect(alias uint64, serverAlias uint64) error {
	log.Println("client connect()")

	s := pion.SettingEngine{}
	s.DetachDataChannels()
	s.SetTrickle(true)
	s.LoggerFactory = &logging.PionLoggingFactory{PeerAlias: alias}
	api := pion.NewAPI(pion.WithSettingEngine(s))

	webRtcConfig := pion.Configuration{ICEServers: client.iceServers}
	conn, err := api.NewPeerConnection(webRtcConfig)
	if err != nil {
		return err
	}

	client.conn = conn

	msg := &protocol.ConnectMessage{Type: protocol.MessageType_CONNECT, ToAlias: serverAlias}
	bytes, err := proto.Marshal(msg)
	if err != nil {
		log.WithError(err).Fatal("cannot marshall connect message")
	}
	client.coordinatorWriteQueue <- bytes

	conn.OnICECandidate(func(candidate *pion.ICECandidate) {
		if candidate == nil {
			log.WithField("peer", alias).Debug("finish collecting candidates")
			return
		}

		iceCandidateInit := candidate.ToJSON()
		serializedCandidate, err := json.Marshal(iceCandidateInit)
		if err != nil {
			log.WithField("peer", alias).WithError(err).Error("cannot serialize candidate")
			return
		}
		msg := protocol.WebRtcMessage{
			Type:    protocol.MessageType_WEBRTC_ICE_CANDIDATE,
			Data:    serializedCandidate,
			ToAlias: alias,
		}
		bytes, err := proto.Marshal(&msg)
		if err != nil {
			log.WithError(err).Fatal("cannot serialize ice candidate message")
		}
		client.coordinatorWriteQueue <- bytes
	})

	conn.OnICEConnectionStateChange(func(connectionState pion.ICEConnectionState) {
		log.Println("ICE Connection State has changed: ", connectionState.String())
		if connectionState == pion.ICEConnectionStateDisconnected {
			if err := conn.Close(); err != nil {
				log.Println("error closing on disconnect", err)
			}
		}
	})

	conn.OnDataChannel(func(d *pion.DataChannel) {
		readPump := func(client *Client, c datachannel.Reader, reliable bool) {
			header := protocol.MessageHeader{}
			buffer := make([]byte, 1024)
			for {
				n, _, err := c.ReadDataChannel(buffer)
				if err != nil {
					log.Println("stop readPump, datachannel closed", reliable)
					return
				}

				if n == 0 {
					log.Println("n=0")
					continue
				}

				bytes := make([]byte, n)
				copy(bytes, buffer[:n])

				if err := proto.Unmarshal(bytes, &header); err != nil {
					log.Println("Failed to load:", err)
					continue
				}

				if client.onMessageReceived != nil {
					client.onMessageReceived(reliable, header.Type, bytes)
				}
			}
		}

		writePump := func(client *Client, c datachannel.Writer, reliable bool) {
			var messagesQueue chan []byte
			var stopQueue chan bool
			if reliable {
				stopQueue = client.StopReliableQueue
				messagesQueue = client.SendReliable
				bytes := <-client.authMessage
				_, err := c.WriteDataChannel(bytes, false)
				if err != nil {
					log.Println("error writing auth message", err)
					return
				}
			} else {
				stopQueue = client.StopUnreliableQueue
				messagesQueue = client.SendUnreliable
			}
			for {
				select {
				case bytes, ok := <-messagesQueue:
					if !ok {
						log.Println("close write pump, channel closed")
						return
					}

					if _, err := c.WriteDataChannel(bytes, false); err != nil {
						log.Println("error writing", err)
						return
					}

					n := len(messagesQueue)
					for i := 0; i < n; i++ {
						bytes = <-messagesQueue
						_, err := c.WriteDataChannel(bytes, false)
						if err != nil {
							log.Println("error writing", err)
							return
						}
					}
				case <-stopQueue:
					log.Println("close write pump, stopQueue")
					return
				}
			}
		}

		d.OnOpen(func() {
			dd, err := d.Detach()
			if err != nil {
				log.Fatal("cannot detach datachannel", err)
			}

			reliable := d.Label() == "reliable"

			if reliable {
				fmt.Println("Data channel open (reliable)")
			} else {
				fmt.Println("Data channel open (unreliable)")
			}
			go readPump(client, dd, reliable)
			go writePump(client, dd, reliable)
		})

	})

	return nil
}

// Start starts a new client
func Start(config *Config) *Client {
	client := MakeClient(config)

	go func() {
		log.Fatal(client.startCoordination())
	}()

	pData := <-client.PeerData

	log.Println("my alias is", pData.Alias)

	if err := client.Connect(pData.Alias, pData.AvailableServers[0]); err != nil {
		log.Fatal(err)
	}

	authMessage, err := config.Auth.GenerateClientAuthMessage()
	if err != nil {
		log.Fatal(err)
	}

	bytes, err := proto.Marshal(authMessage)
	if err != nil {
		log.Fatal(err)
	}

	client.authMessage <- bytes

	return client
}

func (client *Client) startCoordination() error {
	c, _, err := websocket.DefaultDialer.Dial(client.coordinatorURL, nil)
	if err != nil {
		return err
	}

	client.coordinator = c
	defer func() {
		log.Fatal(c.Close())
	}()

	go func() {
		for bytes := range client.coordinatorWriteQueue {
			if err := c.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Fatal("set write deadline error", err)
			}

			if err := c.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
				log.Fatal("write coordinator message", err)
			}
		}
		log.Println("channel closed")
	}()

	header := protocol.CoordinatorMessage{}
	for {
		_, bytes, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return err
		}

		if err := proto.Unmarshal(bytes, &header); err != nil {
			log.Println("Failed to load:", err)
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_WELCOME:
			welcomeMessage := protocol.WelcomeMessage{}
			if err := proto.Unmarshal(bytes, &welcomeMessage); err != nil {
				log.Fatal("Failed to decode welcome message:", err)
			}

			if len(welcomeMessage.AvailableServers) == 0 {
				log.Fatal("no server available to connect")
			}

			client.PeerData <- peerData{
				Alias:            welcomeMessage.Alias,
				AvailableServers: welcomeMessage.AvailableServers,
			}
		case protocol.MessageType_WEBRTC_OFFER:
			webRtcMessage := &protocol.WebRtcMessage{}
			if err := proto.Unmarshal(bytes, webRtcMessage); err != nil {
				log.Error("error unmarshalling webrtc message")
				return err
			}

			log.Println("offer received from: ", webRtcMessage.FromAlias)

			offer := pion.SessionDescription{}
			if err := json.Unmarshal(webRtcMessage.Data, &offer); err != nil {
				log.WithError(err).Fatal("error unmarshalling webrtc message")
			}

			if err := client.conn.SetRemoteDescription(offer); err != nil {
				log.Fatal("error setting remote description", err)
			}

			answer, err := client.conn.CreateAnswer(nil)
			if err != nil {
				log.Fatal("error creating webrtc answer", err)
			}

			if err = client.conn.SetLocalDescription(answer); err != nil {
				log.Fatal("error setting local description", err)
			}

			serializedAnswer, err := json.Marshal(answer)
			if err != nil {
				log.WithError(err).Fatal("cannot serialize answer")
			}

			answerWebRtcMessage := &protocol.WebRtcMessage{
				Type:    protocol.MessageType_WEBRTC_ANSWER,
				Data:    serializedAnswer,
				ToAlias: webRtcMessage.FromAlias,
			}
			bytes, err := proto.Marshal(answerWebRtcMessage)
			if err != nil {
				log.Fatal("encode webrtc answer message failed", err)
			}

			client.coordinatorWriteQueue <- bytes
		case protocol.MessageType_WEBRTC_ICE_CANDIDATE:
			webRtcMessage := &protocol.WebRtcMessage{}
			if err := proto.Unmarshal(bytes, webRtcMessage); err != nil {
				log.Error("error unmarshalling webrtc message")
				return err
			}
			log.Debug("ice candidate received")
			candidate := pion.ICECandidateInit{}
			if err := json.Unmarshal(webRtcMessage.Data, &candidate); err != nil {
				log.WithError(err).Fatal("error unmarshalling candidate")
			}
			if err := client.conn.AddICECandidate(candidate); err != nil {
				log.WithError(err).Fatal("error adding remote ice candidate")
			}
		}
	}
}
