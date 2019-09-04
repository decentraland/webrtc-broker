package commserver

import (
	"sync/atomic"

	"github.com/decentraland/webrtc-broker/internal/logging"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

type peer struct {
	alias    uint64
	identity atomic.Value

	topics map[string]struct{}
	index  int

	services     services
	topicCh      chan topicChange
	messagesCh   chan *peerMessage
	unregisterCh chan *peer

	reliableDC   *DataChannel
	unreliableDC *DataChannel

	reliableRWC    ReadWriteCloser
	reliableBuffer []byte

	unreliableRWC    ReadWriteCloser
	unreliableBuffer []byte

	conn *PeerConnection
	role protocol.Role

	log logging.Logger
}

func (p *peer) GetIdentity() []byte {
	identityAtom := p.identity.Load()
	var identity []byte
	if identityAtom != nil {
		identity = identityAtom.([]byte)
	}

	return identity
}

func (p *peer) IsClosed() bool {
	return p.services.WebRtc.isClosed(p.conn)
}

func (p *peer) Close() {
	err := p.services.WebRtc.close(p.conn)

	if err != nil {
		p.log.Warn().Err(err).Msg("error closing connection")
		return
	}

	p.unregisterCh <- p
}

func (p *peer) readReliablePump() {
	marshaller := p.services.Marshaller
	header := protocol.MessageHeader{}

	if p.reliableBuffer == nil {
		p.reliableBuffer = make([]byte, maxWorldCommMessageSize)
	}

	buffer := p.reliableBuffer
	for {
		n, err := p.reliableRWC.Read(buffer)

		if err != nil {
			p.log.Info().Err(err).Msg("exit peer.readReliablePump(), datachannel closed")
			p.Close()
			return
		}

		if n == 0 {
			continue
		}

		rawMsg := buffer[:n]
		if err := marshaller.Unmarshal(rawMsg, &header); err != nil {
			p.log.Debug().Err(err).Msg("decode header message failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_SUBSCRIPTION:
			p.log().WithField("type", msgType).Info("SUSCRIPTION")
			topicSubscriptionMessage := &protocol.SubscriptionMessage{}
			if err := marshaller.Unmarshal(rawMsg, topicSubscriptionMessage); err != nil {
				p.log.Debug().Err(err).Msg("decode add topic message failure")
				continue
			}

			p.topicCh <- topicChange{
				peer:      p,
				format:    topicSubscriptionMessage.Format,
				rawTopics: topicSubscriptionMessage.Topics,
			}
		case protocol.MessageType_TOPIC:
			p.readTopicMessage(true, rawMsg)
		case protocol.MessageType_TOPIC_IDENTITY:
			p.readTopicIdentityMessage(true, rawMsg)
		case protocol.MessageType_PING:
			if err := p.WriteReliable(rawMsg); err != nil {
				p.log.Debug().Err(err).Msg("error writing ping messag")
			}
		default:
			p.log.Debug().Str("type", msgType.String()).Msg("unhandled reliable message from peer")
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
		n, err := p.unreliableRWC.Read(buffer)

		if err != nil {
			p.log.Info().Err(err).Msg("exit peer.readUnreliablePump(), datachannel closed")
			p.Close()
			return
		}

		if n == 0 {
			continue
		}

		rawMsg := buffer[:n]
		if err := marshaller.Unmarshal(rawMsg, &header); err != nil {
			p.log.Debug().Err(err).Msg("decode header message failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_TOPIC:
			p.readTopicMessage(false, rawMsg)
		case protocol.MessageType_TOPIC_IDENTITY:
			p.readTopicIdentityMessage(false, rawMsg)
		case protocol.MessageType_PING:
			if err := p.WriteUnreliable(rawMsg); err != nil {
				p.log.Debug().Err(err).Msg("error writing ping messag")
			}
		default:
			p.log.Debug().Str("type", msgType.String()).Msg("unhandled unreliable message from peer")
		}
	}
}

func (p *peer) readTopicMessage(reliable bool, rawMsg []byte) {
	log := p.services.Log
	marshaller := p.services.Marshaller
	message := protocol.TopicMessage{}

	if err := marshaller.Unmarshal(rawMsg, &message); err != nil {
		p.log.Debug().Err(err).Msg("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		log.Debug().
			Uint64("peer", p.alias).
			Bool("reliable", reliable).
			Str("topic", message.Topic).
			Msg("message received")
	}

	msg := &peerMessage{
		fromServer: p.role == protocol.Role_COMMUNICATION_SERVER,
		reliable:   reliable,
		topic:      message.Topic,
		from:       p,
	}

	topicFWMessage := protocol.TopicFWMessage{
		Type: protocol.MessageType_TOPIC_FW,
		Body: message.Body,
	}

	if msg.fromServer {
		topicFWMessage.FromAlias = message.FromAlias
	} else {
		topicFWMessage.FromAlias = p.alias
		message.FromAlias = p.alias

		rawMsgToServer, err := marshaller.Marshal(&message)
		if err != nil {
			p.log.Error().Err(err).Msg("encode topic message failure")
			return
		}
		msg.rawMsgToServer = rawMsgToServer
	}

	rawMsgToClient, err := marshaller.Marshal(&topicFWMessage)
	if err != nil {
		p.log.Error().Err(err).Msg("encode topicfwmessage failure")
		return
	}

	msg.rawMsgToClient = rawMsgToClient

	p.messagesCh <- msg
}

func (p *peer) readTopicIdentityMessage(reliable bool, rawMsg []byte) {
	log := p.services.Log
	marshaller := p.services.Marshaller
	message := protocol.TopicIdentityMessage{}

	if err := marshaller.Unmarshal(rawMsg, &message); err != nil {
		p.log.Debug().Err(err).Msg("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		log.Debug().
			Uint64("peer", p.alias).
			Bool("reliable", reliable).
			Str("topic", message.Topic).
			Msg("identity message received")
	}

	msg := &peerMessage{
		fromServer: p.role == protocol.Role_COMMUNICATION_SERVER,
		reliable:   reliable,
		topic:      message.Topic,
		from:       p,
	}

	topicIdentityFWMessage := protocol.TopicIdentityFWMessage{
		Type: protocol.MessageType_TOPIC_IDENTITY_FW,
		Body: message.Body,
	}

	if p.role == protocol.Role_COMMUNICATION_SERVER {
		topicIdentityFWMessage.FromAlias = message.FromAlias
		topicIdentityFWMessage.Identity = message.Identity
		topicIdentityFWMessage.Role = message.Role
	} else {
		topicIdentityFWMessage.FromAlias = p.alias
		message.FromAlias = p.alias

		identity := p.GetIdentity()
		topicIdentityFWMessage.Identity = identity
		message.Identity = identity

		topicIdentityFWMessage.Role = p.role
		message.Role = p.role

		rawMsgToServer, err := marshaller.Marshal(&message)
		if err != nil {
			p.log.Error().Err(err).Msg("encode topic message failure")
			return
		}
		msg.rawMsgToServer = rawMsgToServer
	}

	rawMsgToClient, err := marshaller.Marshal(&topicIdentityFWMessage)
	if err != nil {
		p.log.Error().Err(err).Msg("encode data message failure")
		return
	}

	msg.rawMsgToClient = rawMsgToClient

	p.messagesCh <- msg
}

func (p *peer) WriteReliable(rawMsg []byte) error {
	if p.reliableRWC == nil {
		return nil
	}

	if _, err := p.reliableRWC.Write(rawMsg); err != nil {
		p.log.Error().Err(err).Msg("Error writing reliable channel")
		p.Close()
		return err
	}
	return nil
}

func (p *peer) WriteUnreliable(rawMsg []byte) error {
	if p.unreliableRWC == nil {
		return nil
	}

	if _, err := p.unreliableRWC.Write(rawMsg); err != nil {
		p.log.Error().Err(err).Msg("Error writing unreliable channel")
		p.Close()
		return err
	}
	return nil
}
