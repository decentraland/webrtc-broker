package commserver

import (
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

type peer struct {
	Alias  uint64
	Topics map[string]struct{}
	Index  int

	services        services
	topicQueue      chan topicChange
	messagesQueue   chan *peerMessage
	unregisterQueue chan *peer

	ReliableDC     ReadWriteCloser
	reliableBuffer []byte

	UnreliableDC     ReadWriteCloser
	unreliableBuffer []byte

	serverAlias uint64
	conn        *PeerConnection
	role        protocol.Role
}

func (p *peer) log() *logging.Entry {
	return p.services.Log.
		WithFields(logging.Fields{
			"serverAlias": p.serverAlias,
			"peer":        p.Alias,
		})
}

func (p *peer) logError(err error) *logging.Entry {
	return p.log().WithError(err)
}

func (p *peer) IsClosed() bool {
	return p.services.WebRtc.isClosed(p.conn)
}

func (p *peer) Close() {
	err := p.services.WebRtc.close(p.conn)

	if err != nil {
		p.logError(err).Warn("error closing connection")
		return
	}

	p.unregisterQueue <- p
}

func (p *peer) readReliablePump() {
	marshaller := p.services.Marshaller
	header := protocol.MessageHeader{}

	if p.reliableBuffer == nil {
		p.reliableBuffer = make([]byte, maxWorldCommMessageSize)
	}

	buffer := p.reliableBuffer
	for {
		n, err := p.ReliableDC.Read(buffer)

		if err != nil {
			p.logError(err).Info("exit peer.readReliablePump(), datachannel closed")
			p.Close()
			return
		}

		if n == 0 {
			continue
		}

		rawMsg := buffer[:n]
		if err := marshaller.Unmarshal(rawMsg, &header); err != nil {
			p.logError(err).Debug("decode header message failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_TOPIC_SUBSCRIPTION:
			topicSubscriptionMessage := &protocol.TopicSubscriptionMessage{}
			if err := marshaller.Unmarshal(rawMsg, topicSubscriptionMessage); err != nil {
				p.logError(err).Debug("decode add topic message failure")
				continue
			}

			p.topicQueue <- topicChange{
				peer:      p,
				format:    topicSubscriptionMessage.Format,
				rawTopics: topicSubscriptionMessage.Topics,
			}
		case protocol.MessageType_TOPIC:
			p.readPeerMessage(true, rawMsg)
		case protocol.MessageType_PING:
			p.WriteReliable(rawMsg)
		default:
			p.log().WithField("type", msgType).Debug("unhandled reliable message from peer")
		}
	}
}

func (p *peer) readUnreliablePump() {
	marshaller := p.services.Marshaller

	header := protocol.MessageHeader{}

	if p.unreliableBuffer == nil {
		p.unreliableBuffer = make([]byte, maxWorldCommMessageSize)
	}

	buffer := p.unreliableBuffer
	for {
		n, err := p.UnreliableDC.Read(buffer)

		if err != nil {
			p.logError(err).Info("exit peer.readUnreliablePump(), datachannel closed")
			p.Close()
			return
		}

		if n == 0 {
			continue
		}

		rawMsg := buffer[:n]
		if err := marshaller.Unmarshal(rawMsg, &header); err != nil {
			p.logError(err).Debug("decode header message failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_TOPIC:
			p.readPeerMessage(false, rawMsg)
		case protocol.MessageType_PING:
			p.WriteUnreliable(rawMsg)
		default:
			p.log().WithField("type", msgType).Debug("unhandled unreliable message from peer")
		}
	}
}

func (p *peer) readPeerMessage(reliable bool, rawMsg []byte) {
	log := p.services.Log
	marshaller := p.services.Marshaller
	message := protocol.TopicMessage{}

	if err := marshaller.Unmarshal(rawMsg, &message); err != nil {
		p.logError(err).Debug("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		log.WithFields(logging.Fields{
			"log_type": "message_received",
			"peer":     p.Alias,
			"reliable": reliable,
			"topic":    message.Topic,
		}).Debug("message received")
	}

	msg := &peerMessage{
		fromServer: p.role == protocol.Role_COMMUNICATION_SERVER,
		receivedAt: time.Now(),
		reliable:   reliable,
		topic:      message.Topic,
		from:       p,
	}

	dataMessage := protocol.DataMessage{
		Type: protocol.MessageType_DATA,
		Body: message.Body,
	}

	if p.role == protocol.Role_COMMUNICATION_SERVER {
		dataMessage.FromAlias = message.FromAlias
	} else {
		dataMessage.FromAlias = p.Alias
		message.FromAlias = p.Alias

		rawMsgToServer, err := marshaller.Marshal(&message)
		if err != nil {
			p.logError(err).Error("encode topic message failure")
			return
		}
		msg.rawMsgToServer = rawMsgToServer
	}

	rawMsgToClient, err := marshaller.Marshal(&dataMessage)
	if err != nil {
		p.logError(err).Error("encode data message failure")
		return
	}

	msg.rawMsgToClient = rawMsgToClient

	p.messagesQueue <- msg
}

func (p *peer) WriteReliable(rawMsg []byte) error {
	if _, err := p.ReliableDC.Write(rawMsg); err != nil {
		p.logError(err).Error("Error writing reliable channel")
		p.Close()
		return err
	}
	return nil
}

func (p *peer) WriteUnreliable(rawMsg []byte) error {
	if _, err := p.UnreliableDC.Write(rawMsg); err != nil {
		p.logError(err).Error("Error writing unreliable channel")
		p.Close()
		return err
	}
	return nil
}
