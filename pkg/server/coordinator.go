package server

import (
	"errors"
	"fmt"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/internal/ws"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
)

const (
	pongWait                  = 60 * time.Second
	pingPeriod                = 30 * time.Second
	maxCoordinatorMessageSize = 5000 // NOTE let's adjust this later
	retryCount                = 5
	retryInitialPeriod        = 1 * time.Second
)

type coordinator struct {
	log         logging.Logger
	conn        ws.IWebsocket
	send        chan []byte
	exitOnClose bool
	closed      bool
}

func (c *coordinator) Connect(server *Server, url string) error {
	retryPeriod := retryInitialPeriod

	for retryIndex := 0; retryIndex < retryCount; retryIndex++ {
		conn, err := ws.Dial(url)
		if err == nil {
			c.conn = conn
			return nil
		}

		c.log.Error().Str("url", url).Err(err).Msg("cannot connect to coordinator node")

		if (retryIndex + 1) < retryCount {
			c.log.Debug().
				Str("retry", retryPeriod.String()).
				Msg("wait before retrying coordinator connection")
			time.Sleep(retryPeriod)
		}

		retryPeriod *= 5
	}

	return fmt.Errorf("cannot connect to coordinator after %d retries", retryCount)
}

func (c *coordinator) Send(msg protocol.Message) error {
	log := c.log

	if c.closed {
		return errors.New("coordinator connection is closed")
	}

	bytes, err := proto.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("encode message failure")
		return err
	}

	c.send <- bytes

	return nil
}

func (c *coordinator) readPump(server *Server, welcomeChannel chan *protocol.WelcomeMessage) {
	defer func() {
		c.Close()
	}()

	log := c.log
	c.conn.SetReadLimit(maxCoordinatorMessageSize)

	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Error().Err(err).Msg("Cannot set read deadline")
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
				log.Error().Err(err).Msg("unexcepted close error")
			} else {
				log.Error().Err(err).Msg("read error in coordinator ws")
			}

			break
		}

		if err := proto.Unmarshal(bytes, header); err != nil {
			log.Debug().Err(err).Msg("decode header failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_WELCOME:
			welcomeMessage := &protocol.WelcomeMessage{}
			if err := proto.Unmarshal(bytes, welcomeMessage); err != nil {
				log.Error().Err(err).Msg("decode welcome message failure")
				return
			}

			log.Info().Uint64("alias", welcomeMessage.Alias).Msg("welcome message alias")
			welcomeChannel <- welcomeMessage
		case protocol.MessageType_WEBRTC_OFFER, protocol.MessageType_WEBRTC_ANSWER, protocol.MessageType_WEBRTC_ICE_CANDIDATE:
			webRtcMessage := &protocol.WebRtcMessage{}
			if err := proto.Unmarshal(bytes, webRtcMessage); err != nil {
				log.Debug().Err(err).Msg("decode webrtc message failure")
				continue
			}
			server.webRtcControlCh <- webRtcMessage
		case protocol.MessageType_CONNECT:
			connectMessage := &protocol.ConnectMessage{}
			if err := proto.Unmarshal(bytes, connectMessage); err != nil {
				log.Debug().Err(err).Msg("decode connect message failure")
				continue
			}

			log.Debug().Uint64("to", connectMessage.FromAlias).Msg("Connect message received")
			server.connectCh <- connectMessage.FromAlias
		default:
			log.Debug().Str("type", msgType.String()).Msg("unhandled message from coordinator")
		}
	}
}

func (c *coordinator) writePump() {
	log := c.log
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()

		if err := c.conn.Close(); err != nil {
			log.Debug().Err(err).Msg("error closing connection on writePump exit")
		}
	}()

	for {
		select {
		case bytes, ok := <-c.send:
			if !ok {
				if err := c.conn.WriteCloseMessage(); err != nil {
					log.Debug().Err(err).Msg("error sending write close message")
				}

				log.Info().Msg("channel closed")

				return
			}

			if err := c.conn.WriteMessage(bytes); err != nil {
				log.Error().Err(err).Msg("error writing message")
				return
			}

			n := len(c.send)
			for i := 0; i < n; i++ {
				bytes = <-c.send
				if err := c.conn.WriteMessage(bytes); err != nil {
					log.Error().Err(err).Msg("error writing message")
					return
				}
			}
		case <-ticker.C:
			if err := c.conn.WritePingMessage(); err != nil {
				log.Error().Err(err).Msg("error writing ping message")
				return
			}
		}
	}
}

func (c *coordinator) Close() {
	if c.closed {
		return
	}

	c.closed = true
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			c.log.Debug().Err(err).Msg("error closing coordinator")
		}
	}

	close(c.send)

	if c.exitOnClose {
		c.log.Fatal().Msg("Coordinator connection closed, exiting process")
	}
}
