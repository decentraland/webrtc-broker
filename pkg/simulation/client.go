package simulation

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
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
	Log               logging.Logger
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

	candidatesMux     sync.Mutex
	pendingCandidates []*pion.ICECandidate

	alias       uint64
	serverAlias uint64

	log logging.Logger
}

// MakeClient creates a new client
func MakeClient(config *Config) *Client {
	url, err := config.Auth.GenerateClientConnectURL(config.CoordinatorURL)
	if err != nil {
		config.Log.Fatal().Err(err)
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
		log:                   config.Log,
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

	message := &protocol.SubscriptionMessage{
		Type:   protocol.MessageType_SUBSCRIPTION,
		Format: protocol.Format_PLAIN,
		Topics: buffer.Bytes(),
	}

	bytes, err := proto.Marshal(message)
	if err != nil {
		return err
	}

	client.SendReliable <- bytes

	return nil
}

func (client *Client) signalCandidate(candidate *pion.ICECandidate) {
	iceCandidateInit := candidate.ToJSON()

	serializedCandidate, err := json.Marshal(iceCandidateInit)
	if err != nil {
		client.log.Error().Uint64("peer", client.alias).Msg("cannot serialize candidate")
		return
	}

	msg := protocol.WebRtcMessage{
		Type:    protocol.MessageType_WEBRTC_ICE_CANDIDATE,
		Data:    serializedCandidate,
		ToAlias: client.serverAlias,
	}

	bytes, err := proto.Marshal(&msg)
	if err != nil {
		client.log.Fatal().Err(err).Msg("cannot serialize ice candidate message")
	}

	client.coordinatorWriteQueue <- bytes
}

// Connect connect to specified server
func (client *Client) Connect(alias uint64, serverAlias uint64) error {
	s := pion.SettingEngine{}
	s.DetachDataChannels()
	s.SetTrickle(true)
	s.LoggerFactory = &logging.PionLoggingFactory{DefaultLogLevel: zerolog.WarnLevel, PeerAlias: alias}
	api := pion.NewAPI(pion.WithSettingEngine(s))

	webRtcConfig := pion.Configuration{ICEServers: client.iceServers}

	conn, err := api.NewPeerConnection(webRtcConfig)
	if err != nil {
		return err
	}

	client.conn = conn
	client.alias = alias
	client.serverAlias = serverAlias

	msg := &protocol.ConnectMessage{Type: protocol.MessageType_CONNECT, ToAlias: serverAlias}

	bytes, err := proto.Marshal(msg)
	if err != nil {
		client.log.Fatal().Err(err).Msg("cannot marshall connect message")
	}

	client.coordinatorWriteQueue <- bytes

	conn.OnICECandidate(func(candidate *pion.ICECandidate) {
		if candidate == nil {
			client.log.Debug().Uint64("peer", alias).Msg("finish collecting candidates")
			return
		}

		client.log.Debug().Uint64("peer", alias).Msg("ice candidate found")

		client.candidatesMux.Lock()
		defer client.candidatesMux.Unlock()

		desc := conn.RemoteDescription()
		if desc == nil {
			client.pendingCandidates = append(client.pendingCandidates, candidate)
		} else {
			client.signalCandidate(candidate)
		}
	})

	conn.OnICEConnectionStateChange(func(connectionState pion.ICEConnectionState) {
		client.log.Info().Str("state", connectionState.String()).Msg("ICE Connection State has changed")
		if connectionState == pion.ICEConnectionStateDisconnected ||
			connectionState == pion.ICEConnectionStateFailed {
			if err := conn.Close(); err != nil {
				client.log.Debug().Err(err).Msg("error closing on disconnect")
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
					client.log.Debug().Bool("reliable", reliable).Msg("stop readPump, datachannel closed")
					return
				}

				if n == 0 {
					continue
				}

				bytes := make([]byte, n)
				copy(bytes, buffer[:n])

				if err := proto.Unmarshal(bytes, &header); err != nil {
					client.log.Error().Err(err).Msg("Failed to unmarshall message header")
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
					client.log.Error().Err(err).Msg("error writing auth message")
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
						client.log.Debug().Msg("close write pump, channel closed")
						return
					}

					if _, err := c.WriteDataChannel(bytes, false); err != nil {
						client.log.Error().Err(err).Msg("error writing")
						return
					}

					n := len(messagesQueue)
					for i := 0; i < n; i++ {
						bytes = <-messagesQueue
						_, err := c.WriteDataChannel(bytes, false)
						if err != nil {
							client.log.Error().Err(err).Msg("error writing")
							return
						}
					}
				case <-stopQueue:
					client.log.Debug().Msg("close write pump, stopQueue")
					return
				}
			}
		}

		d.OnOpen(func() {
			dd, err := d.Detach()
			if err != nil {
				client.log.Fatal().Err(err).Msg("cannot detach datachannel")
			}

			reliable := d.Label() == "reliable"

			if reliable {
				client.log.Info().Msg("Data channel open (reliable)")
			} else {
				client.log.Info().Msg("Data channel open (unreliable)")
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
		err := client.startCoordination()
		if err != nil {
			client.log.Fatal().Err(err)
		}
	}()

	pData := <-client.PeerData

	client.log.Info().Msgf("my alias is %d", pData.Alias)

	if len(pData.AvailableServers) == 0 {
		client.log.Fatal().Msg("no available servers")
	}

	if err := client.Connect(pData.Alias, pData.AvailableServers[0]); err != nil {
		client.log.Fatal().Err(err)
	}

	authMessage, err := config.Auth.GenerateClientAuthMessage()
	if err != nil {
		client.log.Fatal().Err(err)
	}

	bytes, err := proto.Marshal(authMessage)
	if err != nil {
		client.log.Fatal().Err(err)
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
		err := c.Close()
		if err != nil {
			client.log.Fatal().Err(err)
		}
	}()

	go func() {
		for bytes := range client.coordinatorWriteQueue {
			if err := c.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				client.log.Fatal().Err(err).Msg("set write deadline error")
			}

			if err := c.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
				client.log.Fatal().Err(err).Msg("write coordinator message")
			}
		}

		client.log.Debug().Msg("channel closed")
	}()

	header := protocol.CoordinatorMessage{}

	for {
		_, bytes, err := c.ReadMessage()
		if err != nil {
			client.log.Error().Err(err).Msg("read message")
			return err
		}

		if err := proto.Unmarshal(bytes, &header); err != nil {
			client.log.Error().Err(err).Msg("failed to unmarshall header")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_WELCOME:
			welcomeMessage := protocol.WelcomeMessage{}
			if err := proto.Unmarshal(bytes, &welcomeMessage); err != nil {
				client.log.Fatal().Err(err).Msg("Failed to decode welcome message")
			}

			if len(welcomeMessage.AvailableServers) == 0 {
				client.log.Fatal().Msg("no server available to connect")
			}

			client.PeerData <- peerData{
				Alias:            welcomeMessage.Alias,
				AvailableServers: welcomeMessage.AvailableServers,
			}
		case protocol.MessageType_WEBRTC_OFFER:
			webRtcMessage := &protocol.WebRtcMessage{}
			if err := proto.Unmarshal(bytes, webRtcMessage); err != nil {
				client.log.Error().Err(err).Msg("error unmarshalling webrtc message")
				return err
			}

			client.log.Debug().Msgf("offer received from: %d", webRtcMessage.FromAlias)

			offer := pion.SessionDescription{}
			if err := json.Unmarshal(webRtcMessage.Data, &offer); err != nil {
				client.log.Fatal().Err(err).Msg("error unmarshalling webrtc message")
			}

			if err := client.conn.SetRemoteDescription(offer); err != nil {
				client.log.Fatal().Err(err).Msg("error setting remote description")
			}

			answer, err := client.conn.CreateAnswer(nil)
			if err != nil {
				client.log.Fatal().Err(err).Msg("error creating webrtc answer")
			}

			serializedAnswer, err := json.Marshal(answer)
			if err != nil {
				client.log.Fatal().Err(err).Msg("cannot serialize answer")
			}

			answerWebRtcMessage := &protocol.WebRtcMessage{
				Type:    protocol.MessageType_WEBRTC_ANSWER,
				Data:    serializedAnswer,
				ToAlias: webRtcMessage.FromAlias,
			}

			bytes, err := proto.Marshal(answerWebRtcMessage)
			if err != nil {
				client.log.Fatal().Err(err).Msg("encode webrtc answer message failed")
			}

			client.log.Debug().Msg("send answer")

			client.coordinatorWriteQueue <- bytes

			if err = client.conn.SetLocalDescription(answer); err != nil {
				client.log.Fatal().Err(err).Msg("error setting local description")
			}

			client.candidatesMux.Lock()
			for _, c := range client.pendingCandidates {
				client.signalCandidate(c)
			}
			client.candidatesMux.Unlock()
		case protocol.MessageType_WEBRTC_ICE_CANDIDATE:
			webRtcMessage := &protocol.WebRtcMessage{}

			if err := proto.Unmarshal(bytes, webRtcMessage); err != nil {
				client.log.Error().Err(err).Msg("error unmarshalling webrtc message")
				return err
			}

			client.log.Debug().Msg("ice candidate received")

			candidate := pion.ICECandidateInit{}
			if err := json.Unmarshal(webRtcMessage.Data, &candidate); err != nil {
				client.log.Fatal().Err(err).Msg("error unmarshalling candidate")
			}

			if err := client.conn.AddICECandidate(candidate); err != nil {
				client.log.Fatal().Err(err).Msg("error adding remote ice candidate")
			}
		case protocol.MessageType_CONNECTION_REFUSED:
			connectionRefusedMessage := &protocol.ConnectionRefusedMessage{}
			if err := proto.Unmarshal(bytes, connectionRefusedMessage); err != nil {
				client.log.Error().Err(err).Msg("error unmarshalling connection refused message")
				return err
			}

			client.log.Info().Str("reason", connectionRefusedMessage.Reason.String()).Msg("connectionRefused")
			client.log.Fatal().Msg("connectionRefused")
		}
	}
}
