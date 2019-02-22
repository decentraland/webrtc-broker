package worldcomm

import (
	"time"

	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/ws"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
)

const (
	writeWait                 = 10 * time.Second
	pongWait                  = 60 * time.Second
	pingPeriod                = 30 * time.Second
	maxCoordinatorMessageSize = 1536 // NOTE let's adjust this later
)

type Coordinator struct {
	log  *logging.Logger
	url  string
	conn ws.IWebsocket
	send chan []byte
}

func makeCoordinator(url string) *Coordinator {
	return &Coordinator{
		log:  logging.New(),
		url:  url,
		send: make(chan []byte, 256),
	}
}

func (c *Coordinator) Connect(state *WorldCommunicationState, authMethod string) error {
	url, err := state.Auth.GenerateAuthURL(authMethod, c.url, protocol.Role_COMMUNICATION_SERVER)

	if err != nil {
		c.log.WithError(err).Error("error generating communication server auth url")
		return err
	}

	conn, err := ws.Dial(url)

	if err != nil {
		c.log.WithFields(logging.Fields{
			"url":   c.url,
			"error": err,
		}).Error("cannot connect to coordinator node")
		return err
	}

	c.conn = conn

	return nil
}

func (c *Coordinator) Send(state *WorldCommunicationState, msg protocol.Message) error {
	log := c.log
	bytes, err := state.marshaller.Marshal(msg)
	if err != nil {
		log.WithError(err).Error("encode message failure")
		return err
	}

	state.agent.RecordSentToCoordinatorSize(len(bytes))
	c.send <- bytes
	return nil
}

func (c *Coordinator) readPump(state *WorldCommunicationState) {
	defer func() {
		c.Close()
		state.stop <- true
	}()

	log := c.log
	marshaller := state.marshaller
	c.conn.SetReadLimit(maxCoordinatorMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(s string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	header := &protocol.CoordinatorMessage{}
	for {
		bytes, err := c.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err) {
				log.WithError(err).Error("unexcepted close error")
			} else {
				log.WithError(err).Error("read error")
			}
			break
		}

		state.agent.RecordReceivedFromCoordinatorSize(len(bytes))
		if err := marshaller.Unmarshal(bytes, header); err != nil {
			log.WithError(err).Debug("decode header failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_WELCOME:
			welcomeMessage := &protocol.WelcomeMessage{}
			if err := marshaller.Unmarshal(bytes, welcomeMessage); err != nil {
				log.WithError(err).Error("decode welcome message failure")
				return
			}

			log.Info("my alias is ", welcomeMessage.Alias)
			state.aliasChannel <- welcomeMessage.Alias
			connectMessage := &protocol.ConnectMessage{Type: protocol.MessageType_CONNECT}

			authMessage, err := state.Auth.GenerateAuthMessage(state.authMethod,
				protocol.Role_COMMUNICATION_SERVER)
			if err != nil {
				log.WithError(err).Error("cannot create auth message (processing welcome)")
				return
			}

			bytes, err := state.marshaller.Marshal(authMessage)
			if err != nil {
				log.WithError(err).Error("cannot encode auth message (processing welcome)")
				return
			}

			for _, alias := range welcomeMessage.AvailableServers {
				connectMessage.ToAlias = alias
				state.coordinator.Send(state, connectMessage)

				p, err := initPeer(state, alias)
				if err != nil {
					log.WithError(err).Error("init peer error creating server (processing welcome)")
					return
				}
				p.sendReliable <- bytes
				p.authenticationSent = true
			}
		case protocol.MessageType_WEBRTC_OFFER, protocol.MessageType_WEBRTC_ANSWER, protocol.MessageType_WEBRTC_ICE_CANDIDATE:
			webRtcMessage := &protocol.WebRtcMessage{}
			if err := marshaller.Unmarshal(bytes, webRtcMessage); err != nil {
				log.WithError(err).Debug("decode webrtc message failure")
				continue
			}
			state.webRtcControlQueue <- webRtcMessage
		case protocol.MessageType_CONNECT:
			connectMessage := &protocol.ConnectMessage{}
			if err := marshaller.Unmarshal(bytes, connectMessage); err != nil {
				log.WithError(err).Debug("decode connect message failure")
				continue
			}
			log.WithFields(logging.Fields{
				"id": state.GetAlias(),
				"to": connectMessage.FromAlias,
			}).Info("Connect message received")
			state.connectQueue <- connectMessage.FromAlias
		default:
			log.WithField("type", msgType).Debug("unhandled message from coordinator")
		}
	}
}

func (c *Coordinator) writePump(state *WorldCommunicationState) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	log := c.log
	for {
		select {
		case bytes, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteCloseMessage()
				log.Info("channel closed")
				return
			}
			if err := c.conn.WriteMessage(bytes); err != nil {
				log.WithError(err).Error("error writing message")
				return
			}

			n := len(c.send)
			for i := 0; i < n; i++ {
				bytes = <-c.send
				if err := c.conn.WriteMessage(bytes); err != nil {
					log.WithError(err).Error("error writing message")
					return
				}
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WritePingMessage(); err != nil {
				log.WithError(err).Error("error writing ping message")
				return
			}
		}
	}
}

func (c *Coordinator) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
	close(c.send)
}
