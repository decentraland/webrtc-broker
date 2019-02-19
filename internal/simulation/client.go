package simulation

import (
	"log"

	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/pions/datachannel"
	"github.com/pions/webrtc"
	"github.com/pions/webrtc/pkg/ice"
)

var webRtcConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
}

type WorldData struct {
	MyAlias          string
	AvailableServers []string
}

type Client struct {
	id                  string
	coordinatorUrl      string
	coordinator         *websocket.Conn
	conn                *webrtc.PeerConnection
	sendReliable        chan []byte
	sendUnreliable      chan []byte
	receivedReliable    chan []byte
	receivedUnreliable  chan []byte
	authMessage         chan []byte
	stopReliableQueue   chan bool
	stopUnreliableQueue chan bool
	worldData           chan WorldData
	topics              map[string]bool
}

func encodeChangeTopicMessage(msgType protocol.MessageType, topic string) ([]byte, error) {
	changeTopicMessage := &protocol.ChangeTopicMessage{
		Type:  msgType,
		Topic: topic,
	}

	bytes, err := proto.Marshal(changeTopicMessage)
	if err != nil {
		return bytes, err
	}

	return bytes, nil
}

func encodeTopicMessage(topic string, data proto.Message) ([]byte, error) {
	body, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	msg := &protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: topic,
		Body:  body,
	}

	bytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func encodeAuthMessage(method string, role protocol.Role, data proto.Message) ([]byte, error) {
	authMessage := protocol.AuthMessage{
		Type:   protocol.MessageType_AUTH,
		Method: method,
		Role:   role,
	}

	bytes, err := proto.Marshal(&authMessage)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func MakeClient(id string, coordinatorUrl string) *Client {
	c := &Client{
		id:                  id,
		coordinatorUrl:      coordinatorUrl,
		authMessage:         make(chan []byte),
		sendReliable:        make(chan []byte, 256),
		sendUnreliable:      make(chan []byte, 256),
		stopReliableQueue:   make(chan bool),
		stopUnreliableQueue: make(chan bool),
		worldData:           make(chan WorldData),
		topics:              make(map[string]bool),
	}

	return c
}

func (client *Client) startCoordination() error {
	c, _, err := websocket.DefaultDialer.Dial(client.coordinatorUrl, nil)
	if err != nil {
		return err
	}

	client.coordinator = c
	defer c.Close()

	for {
		_, bytes, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return err
		}

		header := &protocol.CoordinatorMessage{}
		if err := proto.Unmarshal(bytes, header); err != nil {
			log.Println("Failed to load:", err)
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_WELCOME:
			welcomeMessage := &protocol.WelcomeMessage{}
			if err := proto.Unmarshal(bytes, welcomeMessage); err != nil {
				log.Fatal("Failed to decode welcome message:", err)
			}

			if len(welcomeMessage.AvailableServers) == 0 {
				log.Fatal("no server available to connect")
			}

			client.worldData <- WorldData{
				MyAlias:          welcomeMessage.Alias,
				AvailableServers: welcomeMessage.AvailableServers,
			}
		case protocol.MessageType_WEBRTC_OFFER:
			webRtcMessage := &protocol.WebRtcMessage{}
			if err := proto.Unmarshal(bytes, webRtcMessage); err != nil {
				return err
			}

			log.Println("offer received from: ", webRtcMessage.FromAlias)

			offer := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  webRtcMessage.Sdp,
			}

			if err := client.conn.SetRemoteDescription(offer); err != nil {
				log.Fatal("error setting remote description", err)
			}

			answer, err := client.conn.CreateAnswer(nil)
			if err != nil {
				log.Fatal("error creating webrtc answer", err)
			}

			err = client.conn.SetLocalDescription(answer)
			if err != nil {
				log.Fatal("error setting local description", err)
			}

			answerWebRtcMessage := &protocol.WebRtcMessage{
				Type:    protocol.MessageType_WEBRTC_ANSWER,
				Sdp:     answer.SDP,
				ToAlias: webRtcMessage.FromAlias,
			}
			bytes, err := proto.Marshal(answerWebRtcMessage)
			if err != nil {
				log.Fatal("encode webrtc answer message failed", err)
			}
			if err := client.coordinator.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
				log.Fatal("write answer message", err)
			}
		}
	}
}

func (client *Client) connect(serverAlias string) error {
	log.Println("client connect()")

	conn, err := webrtc.NewPeerConnection(webRtcConfig)
	if err != nil {
		return err
	}

	client.conn = conn

	msg := &protocol.ConnectMessage{Type: protocol.MessageType_CONNECT, ToAlias: serverAlias}
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	if err := client.coordinator.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
		return err
	}

	conn.OnICEConnectionStateChange(func(connectionState ice.ConnectionState) {
		log.Println("ICE Connection State has changed: ", connectionState.String())
		if connectionState == ice.ConnectionStateDisconnected {
			conn.Close()
		}
	})

	conn.OnDataChannel(func(d *webrtc.DataChannel) {

		readPump := func(client *Client, c *datachannel.DataChannel, reliable bool) {
			var received chan []byte

			if reliable {
				received = client.receivedReliable
			} else {
				received = client.receivedUnreliable
			}

			for {
				buffer := make([]byte, 1024)
				n, err := c.Read(buffer)
				if err != nil {
					log.Println("stop readPump, datachannel closed", reliable)
					return
				}

				if n == 0 {
					log.Println("n=0")
					continue
				}

				bytes := buffer[:n]
				header := &protocol.WorldCommMessage{}
				if err := proto.Unmarshal(bytes, header); err != nil {
					log.Println("Failed to load:", err)
					continue
				}

				if received != nil {
					received <- bytes
				}
			}
		}

		writePump := func(client *Client, c *datachannel.DataChannel, reliable bool) {
			var messagesQueue chan []byte
			var stopQueue chan bool
			if reliable {
				stopQueue = client.stopReliableQueue
				messagesQueue = client.sendReliable
				bytes := <-client.authMessage
				_, err := c.Write(bytes)
				if err != nil {
					log.Println("error writting auth message", err)
					return
				}
			} else {
				stopQueue = client.stopUnreliableQueue
				messagesQueue = client.sendUnreliable
			}
			for {
				select {
				case bytes, ok := <-messagesQueue:
					if !ok {
						log.Println("close write pump, channel closed")
						return
					}

					_, err := c.Write(bytes)
					if err != nil {
						log.Println("error writting", err)
						return
					}
					n := len(messagesQueue)

					for i := 0; i < n; i++ {
						bytes := <-messagesQueue
						_, err := c.Write(bytes)
						if err != nil {
							log.Println("error writting", err)
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
				log.Fatal("cannot detach datachannel")
			}

			reliable := d.Label == "reliable"

			if reliable {
				log.Println("Data channel open (reliable)")
			} else {
				log.Println("Data channel open (unreliable)")
			}
			go readPump(client, dd, reliable)
			go writePump(client, dd, reliable)
		})

	})

	return nil
}

func (client *Client) sendAddTopicMessage(topic string) error {
	bytes, err := encodeChangeTopicMessage(protocol.MessageType_ADD_TOPIC, topic)

	if err != nil {
		return err
	}

	client.sendReliable <- bytes
	return nil
}

func (client *Client) sendRemoveTopicMessage(topic string) error {
	bytes, err := encodeChangeTopicMessage(protocol.MessageType_REMOVE_TOPIC, topic)

	if err != nil {
		return err
	}

	client.sendReliable <- bytes

	return nil
}
