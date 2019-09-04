package commserver

import (
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/internal/ws"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

const (
	writeWait                 = 10 * time.Second
	pongWait                  = 60 * time.Second
	pingPeriod                = 30 * time.Second
	maxCoordinatorMessageSize = 5000 // NOTE let's adjust this later
)

type coordinator struct {
	log         *logging.Logger
	url         string
	conn        ws.IWebsocket
	send        chan []byte
	exitOnClose bool
}

func (c *coordinator) Connect(state *State) error {
	url, err := state.services.Auth.GenerateServerConnectURL(c.url)
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

func (c *coordinator) Send(state *State, msg protocol.Message) error {
	log := c.log
	bytes, err := state.services.Marshaller.Marshal(msg)
	if err != nil {
		log.WithError(err).Error("encode message failure")
		return err
	}

	c.send <- bytes
	return nil
}

func (c *coordinator) readPump(state *State, welcomeChannel chan *protocol.WelcomeMessage) {
	defer func() {
		c.Close()
		state.stop <- true
	}()

	log := c.log
	marshaller := state.services.Marshaller
	c.conn.SetReadLimit(maxCoordinatorMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.WithError(err).Error("Cannot set read deadline")
		return
	}
	c.conn.SetPongHandler(func(s string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
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
			welcomeChannel <- welcomeMessage
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
			log.WithFields(logging.Fields{"to": connectMessage.FromAlias}).Debug("Connect message received XXXX")
			state.connectQueue <- connectMessage.FromAlias
		default:
			log.WithField("type", msgType).Debug("unhandled message from coordinator")
		}
	}
}

func (c *coordinator) writePump(_ *State) {
	log := c.log
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if err := c.conn.Close(); err != nil {
			log.WithError(err).Debug("error closing connection on writePump exit")
		}
	}()

	for {
		select {
		case bytes, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.WithError(err).Error("error setting write deadline")
				return
			}

			if !ok {
				if err := c.conn.WriteCloseMessage(); err != nil {
					log.WithError(err).Debug("error sending write close message")
				}
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
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.WithError(err).Error("error setting write deadline")
				return
			}

			if err := c.conn.WritePingMessage(); err != nil {
				log.WithError(err).Error("error writing ping message")
				return
			}
		}
	}
}

func (c *coordinator) Close() {
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			c.log.WithError(err).Debug("error closing coordinator")
		}
	}
	close(c.send)

	if c.exitOnClose {
		// TODO reconnect with circuit breaker instead
		c.log.Fatal("Coordinator connection closed, exiting process")
	}
}
