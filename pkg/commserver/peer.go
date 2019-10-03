package commserver

import (
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/decentraland/webrtc-broker/internal/logging"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

// PeerWriter represents the basic write operations for a peer dc
type PeerWriter interface {
	BufferedAmount() uint64
	Write(p []byte) error
}

type reliablePeerWriter struct {
	p *peer
}

func (w *reliablePeerWriter) BufferedAmount() uint64 {
	return w.p.reliableDC.BufferedAmount()
}

func (w *reliablePeerWriter) Write(p []byte) error {
	if len(p) == 0 {
		debug.PrintStack()
		w.p.log.Fatal().Msg("trying to write an empty message to a reliable channel")
	}
	w.p.reliableRWCMutex.RLock()
	reliableRWC := w.p.reliableRWC
	w.p.reliableRWCMutex.RUnlock()

	if reliableRWC != nil {
		_, err := reliableRWC.Write(p)
		if err != nil {
			w.p.log.Error().Err(err).Msg("Error writing reliable datachannel")
			w.p.Close()
			return err
		}
	}
	return nil
}

type unreliablePeerWriter struct {
	p *peer
}

func (w *unreliablePeerWriter) BufferedAmount() uint64 {
	return w.p.unreliableDC.BufferedAmount()
}

func (w *unreliablePeerWriter) Write(p []byte) error {
	if len(p) == 0 {
		debug.PrintStack()
		w.p.log.Fatal().Msg("trying to write an empty message to a unreliable channel")
	}
	w.p.unreliableRWCMutex.RLock()
	unreliableRWC := w.p.unreliableRWC
	w.p.unreliableRWCMutex.RUnlock()

	if unreliableRWC != nil {
		_, err := unreliableRWC.Write(p)
		if err != nil {
			w.p.log.Error().Err(err).Msg("Error writing unreliable datachannel")
			w.p.Close()
			return err
		}
	}

	return nil
}

// WriterController is in charge of the peer writer flow control
type WriterController interface {
	Write(byte []byte)
	OnBufferedAmountLow()
}

type peer struct {
	alias    uint64
	identity atomic.Value

	topics map[string]struct{}
	index  int

	services     services
	topicCh      chan topicChange
	messagesCh   chan *peerMessage
	unregisterCh chan *peer

	reliableDC       *DataChannel
	reliableRWCMutex sync.RWMutex
	reliableRWC      ReadWriteCloser
	reliableBuffer   []byte
	reliableWriter   WriterController

	unreliableDC       *DataChannel
	unreliableRWCMutex sync.RWMutex
	unreliableRWC      ReadWriteCloser
	unreliableBuffer   []byte
	unreliableWriter   WriterController

	conn *PeerConnection
	role protocol.Role

	candidatesMux     sync.Mutex
	pendingCandidates []*ICECandidate

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

	p.reliableRWCMutex.RLock()
	reliableRWC := p.reliableRWC
	p.reliableRWCMutex.RUnlock()
	for {
		n, err := reliableRWC.Read(buffer)

		if err != nil {
			p.log.Info().Err(err).Msg("exit peer.readReliablePump(), datachannel closed")
			p.Close()
			return
		}

		if n == 0 {
			p.log.Debug().Msg("0 bytes read")
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
			topicSubscriptionMessage := &protocol.SubscriptionMessage{}
			if err := marshaller.Unmarshal(rawMsg, topicSubscriptionMessage); err != nil {
				p.log.Debug().Err(err).Msg("decode add topic message failure")
				continue
			}

			if verbose {
				p.log.Debug().
					Str("message type", "subscription").
					Bool("reliable", true).
					Msg("got a new message")
			}

			p.topicCh <- topicChange{
				peer:      p,
				format:    topicSubscriptionMessage.Format,
				rawTopics: topicSubscriptionMessage.Topics,
			}
		case protocol.MessageType_TOPIC:
			if verbose {
				p.log.Debug().
					Str("message type", "topic").
					Bool("reliable", true).
					Msg("got a new message")
			}
			p.readTopicMessage(true, rawMsg)
		case protocol.MessageType_TOPIC_IDENTITY:
			if verbose {
				p.log.Debug().
					Str("message type", "topic identity").
					Bool("reliable", true).
					Msg("got a new message")
			}
			p.readTopicIdentityMessage(true, rawMsg)
		case protocol.MessageType_PING:
			if verbose {
				p.log.Debug().
					Str("message type", "ping").
					Bool("reliable", true).
					Msg("got a new message")
			}
			p.WriteReliable(rawMsg)
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

	p.unreliableRWCMutex.RLock()
	unreliableRWC := p.unreliableRWC
	p.unreliableRWCMutex.RUnlock()

	buffer := p.unreliableBuffer
	for {
		n, err := unreliableRWC.Read(buffer)

		if err != nil {
			p.log.Info().Err(err).Msg("exit peer.readUnreliablePump(), datachannel closed")
			p.Close()
			return
		}

		if n == 0 {
			p.log.Debug().Msg("0 bytes read")
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
			if verbose {
				p.log.Debug().
					Str("message type", "topic").
					Bool("reliable", false).
					Msg("got a new message")
			}
			p.readTopicMessage(false, rawMsg)
		case protocol.MessageType_TOPIC_IDENTITY:
			if verbose {
				p.log.Debug().
					Str("message type", "topic identity").
					Bool("reliable", false).
					Msg("got a new message")
			}
			p.readTopicIdentityMessage(false, rawMsg)
		case protocol.MessageType_PING:
			if verbose {
				p.log.Debug().
					Str("message type", "ping").
					Bool("reliable", false).
					Msg("got a new message")
			}
			p.WriteUnreliable(rawMsg)
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
	}

	rawMsgToServer, err := marshaller.Marshal(&message)
	if err != nil {
		p.log.Error().Err(err).Msg("encode topic message failure")
		return
	}

	msg.rawMsgToServer = rawMsgToServer
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

func (p *peer) WriteReliable(rawMsg []byte) {
	p.reliableWriter.Write(rawMsg)
}

func (p *peer) WriteUnreliable(rawMsg []byte) {
	p.unreliableWriter.Write(rawMsg)
}
