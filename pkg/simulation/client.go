package simulation

import (
	"bytes"
	"log"
	"time"

	"github.com/decentraland/webrtc-broker/internal/utils"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"
	pionlogging "github.com/pion/logging"
	"github.com/pion/webrtc/v2"
	"github.com/sirupsen/logrus"
)

const (
	writeWait = 60 * time.Second
)

var webRtcConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
}

type WorldData struct {
	MyAlias          uint64
	AvailableServers []uint64
}

type ReceivedMessage struct {
	Type       protocol.MessageType
	RawMessage []byte
}

type Client struct {
	id                 string
	coordinatorURL     string
	coordinator        *websocket.Conn
	conn               *webrtc.PeerConnection
	sendReliable       chan []byte
	sendUnreliable     chan []byte
	receivedReliable   chan ReceivedMessage
	receivedUnreliable chan ReceivedMessage
	authMessage        chan []byte

	coordinatorWriteQueue chan []byte
	stopReliableQueue     chan bool
	stopUnreliableQueue   chan bool
	worldData             chan WorldData
	topics                map[string]bool
}

func encodeTopicSubscriptionMessage(topics map[string]bool) ([]byte, error) {
	buffer := bytes.Buffer{}

	i := 0
	last := len(topics) - 1
	for topic := range topics {
		buffer.WriteString(topic)
		if i != last {
			buffer.WriteString(" ")
		}
		i += 1
	}

	gzip := utils.GzipCompression{}
	encodedTopics, err := gzip.Zip(buffer.Bytes())
	if err != nil {
		return nil, err
	}

	message := &protocol.TopicSubscriptionMessage{
		Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
		Format: protocol.Format_GZIP,
		Topics: encodedTopics,
	}

	bytes, err := proto.Marshal(message)
	if err != nil {
		return nil, err
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

func MakeClient(id string, coordinatorURL string) *Client {
	c := &Client{
		id:                    id,
		coordinatorURL:        coordinatorURL,
		authMessage:           make(chan []byte),
		sendReliable:          make(chan []byte, 256),
		sendUnreliable:        make(chan []byte, 256),
		stopReliableQueue:     make(chan bool),
		stopUnreliableQueue:   make(chan bool),
		worldData:             make(chan WorldData),
		topics:                make(map[string]bool),
		coordinatorWriteQueue: make(chan []byte, 256),
	}

	go c.CoordinatorWritePump()
	return c
}

func (client *Client) CoordinatorWritePump() {
	defer func() {
		client.coordinator.Close()
	}()

	for {
		select {
		case bytes, ok := <-client.coordinatorWriteQueue:
			client.coordinator.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				log.Println("channel closed")
				return
			}

			if err := client.coordinator.WriteMessage(websocket.BinaryMessage, bytes); err != nil {
				log.Fatal("write coordinator message", err)
			}
		}
	}
}

func (client *Client) startCoordination() error {
	c, _, err := websocket.DefaultDialer.Dial(client.coordinatorURL, nil)
	if err != nil {
		return err
	}

	client.coordinator = c
	defer c.Close()

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

			client.coordinatorWriteQueue <- bytes
		}
	}
}

type LogrusLevelLogger struct {
	log       *logrus.Logger
	peerAlias uint64
}

func (lll *LogrusLevelLogger) Trace(msg string) { lll.log.WithField("peer", lll.peerAlias).Trace(msg) }
func (lll *LogrusLevelLogger) Error(msg string) { lll.log.WithField("peer", lll.peerAlias).Error(msg) }
func (lll *LogrusLevelLogger) Debug(msg string) { lll.log.WithField("peer", lll.peerAlias).Debug(msg) }
func (lll *LogrusLevelLogger) Info(msg string)  { lll.log.WithField("peer", lll.peerAlias).Info(msg) }
func (lll *LogrusLevelLogger) Warn(msg string)  { lll.log.WithField("peer", lll.peerAlias).Warn(msg) }

func (lll *LogrusLevelLogger) Tracef(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Tracef(format, args...)
}
func (lll *LogrusLevelLogger) Debugf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Debugf(format, args...)
}
func (lll *LogrusLevelLogger) Infof(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Infof(format, args...)
}
func (lll *LogrusLevelLogger) Warnf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Warnf(format, args...)
}
func (lll *LogrusLevelLogger) Errorf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Errorf(format, args...)
}

type PionSimulationLoggingFactory struct {
	PeerAlias uint64
}

func (f *PionSimulationLoggingFactory) NewLogger(scope string) pionlogging.LeveledLogger {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	return &LogrusLevelLogger{log: log, peerAlias: f.PeerAlias}
}

func (client *Client) connect(alias uint64, serverAlias uint64) error {
	log.Println("client connect()")

	s := webrtc.SettingEngine{}
	s.DetachDataChannels()
	s.LoggerFactory = &PionSimulationLoggingFactory{PeerAlias: alias}

	api := webrtc.NewAPI(webrtc.WithSettingEngine(s))

	conn, err := api.NewPeerConnection(webRtcConfig)
	if err != nil {
		return err
	}

	client.conn = conn

	msg := &protocol.ConnectMessage{Type: protocol.MessageType_CONNECT, ToAlias: serverAlias}
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	client.coordinatorWriteQueue <- bytes

	conn.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Println("ICE Connection State has changed: ", connectionState.String())
		if connectionState == webrtc.ICEConnectionStateDisconnected {
			conn.Close()
		}
	})

	conn.OnDataChannel(func(d *webrtc.DataChannel) {

		readPump := func(client *Client, c datachannel.Reader, reliable bool) {
			var received chan ReceivedMessage

			if reliable {
				received = client.receivedReliable
			} else {
				received = client.receivedUnreliable
			}

			header := protocol.WorldCommMessage{}
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

				if received != nil {
					received <- ReceivedMessage{Type: header.Type, RawMessage: bytes}
				}
			}
		}

		writePump := func(client *Client, c datachannel.Writer, reliable bool) {
			var messagesQueue chan []byte
			var stopQueue chan bool
			if reliable {
				stopQueue = client.stopReliableQueue
				messagesQueue = client.sendReliable
				bytes := <-client.authMessage
				_, err := c.WriteDataChannel(bytes, false)
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

					_, err := c.WriteDataChannel(bytes, false)
					if err != nil {
						log.Println("error writting", err)
						return
					}

					n := len(messagesQueue)
					for i := 0; i < n; i++ {
						bytes = <-messagesQueue
						_, err := c.WriteDataChannel(bytes, false)
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

			reliable := d.Label() == "reliable"

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

func (client *Client) sendTopicSubscriptionMessage(topics map[string]bool) error {
	bytes, err := encodeTopicSubscriptionMessage(topics)

	if err != nil {
		return err
	}

	client.sendReliable <- bytes
	return nil
}
