// Package broker implements the actual webrtc broker
package broker

import (
	"bytes"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/decentraland/webrtc-broker/pkg/server"
	"github.com/golang/protobuf/proto"
	"github.com/pion/datachannel"
	pion "github.com/pion/webrtc/v2"
)

const (
	defaultMaxPeerBufferSize                           uint64 = 1024 * 1024 // 1 MB
	defaultReliableChannelBufferedAmountLowThreshold          = 0
	defaultUnreliableChannelBufferedAmountLowThreshold        = 0
	maxWorldCommMessageSize                                   = 5120
	logTopicMessageReceived                                   = false
	verbose                                                   = false
)

// Broker ...
type Broker struct {
	*server.Server
	role           protocol.Role
	subscriptionCh chan subscriptionChange
	messagesCh     chan *peerMessage

	subscriptions        topicSubscriptions
	subscriptionsLock    sync.RWMutex
	initiatedConnections map[uint64]protocol.Role
	peers                map[uint64]*peer
	peersMux             sync.Mutex

	coordinatorURL string
	auth           authentication.ServerAuthenticator

	zipper                                      ZipCompression
	reliableWriterControllerFactory             WriterControllerFactory
	unreliableWriterControllerFactory           WriterControllerFactory
	reliableChannelBufferedAmountLowThreshold   uint64
	unreliableChannelBufferedAmountLowThreshold uint64

	log logging.Logger
}

// Config is the broker config
type Config struct {
	CoordinatorURL                              string
	Log                                         *logging.Logger
	ICEServers                                  []pion.ICEServer
	Zipper                                      ZipCompression
	Auth                                        authentication.ServerAuthenticator
	ReliableWriterControllerFactory             WriterControllerFactory
	UnreliableWriterControllerFactory           WriterControllerFactory
	ReliableChannelBufferedAmountLowThreshold   uint64
	UnreliableChannelBufferedAmountLowThreshold uint64

	MaxPeers               uint16
	ExitOnCoordinatorClose bool
	WebRtcLogLevel         zerolog.Level
	Role                   protocol.Role
}

type peer struct {
	*server.Peer

	identity atomic.Value
	role     int32

	topics map[string]struct{}

	subscriptionCh chan subscriptionChange
	messagesCh     chan *peerMessage

	reliableDC       *pion.DataChannel
	reliableRWCMutex sync.RWMutex
	reliableRWC      datachannel.ReadWriteCloser
	reliableBuffer   []byte
	reliableWriter   WriterController

	unreliableDC       *pion.DataChannel
	unreliableRWCMutex sync.RWMutex
	unreliableRWC      datachannel.ReadWriteCloser
	unreliableBuffer   []byte
	unreliableWriter   WriterController
}

func (p *peer) getRole() protocol.Role {
	return protocol.Role(atomic.LoadInt32(&p.role))
}

func (p *peer) GetIdentity() []byte {
	identityAtom := p.identity.Load()

	var identity []byte
	if identityAtom != nil {
		identity = identityAtom.([]byte)
	}

	return identity
}

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
		w.p.Log.Fatal().Msg("trying to write an empty message to a reliable channel")
	}

	w.p.reliableRWCMutex.RLock()
	reliableRWC := w.p.reliableRWC
	w.p.reliableRWCMutex.RUnlock()

	if reliableRWC != nil {
		_, err := reliableRWC.Write(p)
		if err != nil {
			w.p.Log.Error().Err(err).Msg("Error writing reliable datachannel")
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
		w.p.Log.Fatal().Msg("trying to write an empty message to a unreliable channel")
	}

	w.p.unreliableRWCMutex.RLock()
	unreliableRWC := w.p.unreliableRWC
	w.p.unreliableRWCMutex.RUnlock()

	if unreliableRWC != nil {
		_, err := unreliableRWC.Write(p)
		if err != nil {
			w.p.Log.Error().Err(err).Msg("Error writing unreliable datachannel")
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

func (p *peer) readReliablePump() {
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
			p.Log.Info().Err(err).Msg("exit peer.readReliablePump(), datachannel closed")
			p.Close()

			return
		}

		if n == 0 {
			p.Log.Debug().Msg("0 bytes read")
			continue
		}

		rawMsg := buffer[:n]
		if err := proto.Unmarshal(rawMsg, &header); err != nil {
			p.Log.Debug().Err(err).Msg("decode header message failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_SUBSCRIPTION:
			topicSubscriptionMessage := &protocol.SubscriptionMessage{}
			if err := proto.Unmarshal(rawMsg, topicSubscriptionMessage); err != nil {
				p.Log.Debug().Err(err).Msg("decode add topic message failure")
				continue
			}

			if verbose {
				p.Log.Debug().
					Str("message type", "subscription").
					Bool("reliable", true).
					Msg("got a new message")
			}

			p.subscriptionCh <- subscriptionChange{
				peer:      p,
				format:    topicSubscriptionMessage.Format,
				rawTopics: topicSubscriptionMessage.Topics,
			}
		case protocol.MessageType_TOPIC:
			if verbose {
				p.Log.Debug().
					Str("message type", "topic").
					Bool("reliable", true).
					Msg("got a new message")
			}

			p.readTopicMessage(true, rawMsg)
		case protocol.MessageType_TOPIC_IDENTITY:
			if verbose {
				p.Log.Debug().
					Str("message type", "topic identity").
					Bool("reliable", true).
					Msg("got a new message")
			}

			p.readTopicIdentityMessage(true, rawMsg)
		case protocol.MessageType_PING:
			if verbose {
				p.Log.Debug().
					Str("message type", "ping").
					Bool("reliable", true).
					Msg("got a new message")
			}

			p.WriteReliable(rawMsg)
		default:
			p.Log.Debug().Str("type", msgType.String()).Msg("unhandled reliable message from peer")
		}
	}
}

func (p *peer) readUnreliablePump() {
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
			p.Log.Info().Err(err).Msg("exit peer.readUnreliablePump(), datachannel closed")
			p.Close()

			return
		}

		if n == 0 {
			p.Log.Debug().Msg("0 bytes read")
			continue
		}

		rawMsg := buffer[:n]
		if err := proto.Unmarshal(rawMsg, &header); err != nil {
			p.Log.Debug().Err(err).Msg("decode header message failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_TOPIC:
			if verbose {
				p.Log.Debug().
					Str("message type", "topic").
					Bool("reliable", false).
					Msg("got a new message")
			}

			p.readTopicMessage(false, rawMsg)
		case protocol.MessageType_TOPIC_IDENTITY:
			if verbose {
				p.Log.Debug().
					Str("message type", "topic identity").
					Bool("reliable", false).
					Msg("got a new message")
			}

			p.readTopicIdentityMessage(false, rawMsg)
		case protocol.MessageType_PING:
			if verbose {
				p.Log.Debug().
					Str("message type", "ping").
					Bool("reliable", false).
					Msg("got a new message")
			}

			p.WriteUnreliable(rawMsg)
		default:
			p.Log.Debug().Str("type", msgType.String()).Msg("unhandled unreliable message from peer")
		}
	}
}

func (p *peer) readTopicMessage(reliable bool, rawMsg []byte) {
	message := protocol.TopicMessage{}

	if err := proto.Unmarshal(rawMsg, &message); err != nil {
		p.Log.Debug().Err(err).Msg("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		p.Log.Debug().
			Uint64("peer", p.Alias).
			Bool("reliable", reliable).
			Str("topic", message.Topic).
			Msg("message received")
	}

	msg := &peerMessage{
		fromServer: p.getRole() == protocol.Role_COMMUNICATION_SERVER,
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
		topicFWMessage.FromAlias = p.Alias
		message.FromAlias = p.Alias
	}

	rawMsgToServer, err := proto.Marshal(&message)
	if err != nil {
		p.Log.Error().Err(err).Msg("encode topic message failure")
		return
	}

	msg.rawMsgToServer = rawMsgToServer

	rawMsgToClient, err := proto.Marshal(&topicFWMessage)
	if err != nil {
		p.Log.Error().Err(err).Msg("encode topicfwmessage failure")
		return
	}

	msg.rawMsgToClient = rawMsgToClient

	p.messagesCh <- msg
}

func (p *peer) readTopicIdentityMessage(reliable bool, rawMsg []byte) {
	message := protocol.TopicIdentityMessage{}

	if err := proto.Unmarshal(rawMsg, &message); err != nil {
		p.Log.Debug().Err(err).Msg("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		p.Log.Debug().
			Uint64("peer", p.Alias).
			Bool("reliable", reliable).
			Str("topic", message.Topic).
			Msg("identity message received")
	}

	role := p.getRole()
	msg := &peerMessage{
		fromServer: role == protocol.Role_COMMUNICATION_SERVER,
		reliable:   reliable,
		topic:      message.Topic,
		from:       p,
	}

	topicIdentityFWMessage := protocol.TopicIdentityFWMessage{
		Type: protocol.MessageType_TOPIC_IDENTITY_FW,
		Body: message.Body,
	}

	if role == protocol.Role_COMMUNICATION_SERVER {
		topicIdentityFWMessage.FromAlias = message.FromAlias
		topicIdentityFWMessage.Identity = message.Identity
		topicIdentityFWMessage.Role = message.Role
	} else {
		topicIdentityFWMessage.FromAlias = p.Alias
		message.FromAlias = p.Alias

		identity := p.GetIdentity()
		topicIdentityFWMessage.Identity = identity
		message.Identity = identity

		topicIdentityFWMessage.Role = role
		message.Role = role

		rawMsgToServer, err := proto.Marshal(&message)
		if err != nil {
			p.Log.Error().Err(err).Msg("encode topic message failure")
			return
		}
		msg.rawMsgToServer = rawMsgToServer
	}

	rawMsgToClient, err := proto.Marshal(&topicIdentityFWMessage)
	if err != nil {
		p.Log.Error().Err(err).Msg("encode data message failure")
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

type subscriptionChange struct {
	peer      *peer
	format    protocol.Format
	rawTopics []byte
}

type peerMessage struct {
	fromServer     bool
	reliable       bool
	topic          string
	from           *peer
	rawMsgToServer []byte
	rawMsgToClient []byte
}

type topicSubscription struct {
	clients []*peer
	servers []*peer
}

func (s *topicSubscription) isEmpty() bool {
	return len(s.clients) == 0 && len(s.servers) == 0
}

type topicSubscriptions map[string]*topicSubscription

func (ts *topicSubscriptions) buildTopicsBuffer() ([]byte, error) {
	buffer := bytes.Buffer{}
	i := 0
	last := len(*ts) - 1

	for topic := range *ts {
		if _, err := buffer.WriteString(topic); err != nil {
			return []byte{}, err
		}

		if i != last {
			if _, err := buffer.WriteString(" "); err != nil {
				return []byte{}, err
			}
		}
		i++
	}

	return buffer.Bytes(), nil
}

func (ts *topicSubscriptions) AddClientSubscription(topic string, client *peer) (subscriptionChanged bool) {
	s, ok := (*ts)[topic]
	if ok {
		s.clients = append(s.clients, client)
		return false
	}

	(*ts)[topic] = &topicSubscription{
		clients: []*peer{client},
		servers: make([]*peer, 0),
	}

	return true
}

func (ts *topicSubscriptions) AddServerSubscription(topic string, server *peer) (subscriptionChanged bool) {
	s, ok := (*ts)[topic]

	if ok {
		s.servers = append(s.servers, server)
		return false
	}

	(*ts)[topic] = &topicSubscription{
		clients: make([]*peer, 0),
		servers: []*peer{server},
	}

	return true
}

func removePeer(peers []*peer, p *peer) []*peer {
	for i, peer := range peers {
		if p == peer {
			size := len(peers)
			peers[size-1], peers[i] = peers[i], peers[size-1]

			return peers[:size-1]
		}
	}

	return peers
}

func (ts *topicSubscriptions) RemoveClientSubscription(topic string, client *peer) (subscriptionChanged bool) {
	s, ok := (*ts)[topic]

	if !ok {
		return false
	}

	s.clients = removePeer(s.clients, client)

	if s.isEmpty() {
		delete(*ts, topic)
		return true
	}

	return false
}

func (ts *topicSubscriptions) RemoveServerSubscription(topic string, server *peer) (subscriptionChanged bool) {
	s, ok := (*ts)[topic]

	if !ok {
		return false
	}

	s.servers = removePeer(s.servers, server)

	if s.isEmpty() {
		delete(*ts, topic)
		return true
	}

	return false
}

// NewBroker creates a new broker
func NewBroker(config *Config) (*Broker, error) {
	if config.Role != protocol.Role_COMMUNICATION_SERVER &&
		config.Role != protocol.Role_COMMUNICATION_SERVER_HUB {
		return nil, fmt.Errorf("invalid server role %s", config.Role.String())
	}

	var log logging.Logger
	if config.Log == nil {
		log = logging.New()
	} else {
		log = *config.Log
	}

	if config.Auth == nil {
		return nil, errors.New("config.Auth cannot be nil")
	}

	broker := &Broker{
		subscriptions:  make(topicSubscriptions),
		subscriptionCh: make(chan subscriptionChange, 255),
		messagesCh:     make(chan *peerMessage, 255),
		log:            log,

		initiatedConnections:              make(map[uint64]protocol.Role),
		peers:                             make(map[uint64]*peer),
		auth:                              config.Auth,
		coordinatorURL:                    config.CoordinatorURL,
		zipper:                            config.Zipper,
		role:                              config.Role,
		reliableWriterControllerFactory:   config.ReliableWriterControllerFactory,
		unreliableWriterControllerFactory: config.UnreliableWriterControllerFactory,
		reliableChannelBufferedAmountLowThreshold:   config.ReliableChannelBufferedAmountLowThreshold,
		unreliableChannelBufferedAmountLowThreshold: config.UnreliableChannelBufferedAmountLowThreshold,
	}

	var err error

	broker.Server, err = server.NewServer(&server.Config{
		WebRtcLogLevel:         config.WebRtcLogLevel,
		Log:                    &log,
		ICEServers:             config.ICEServers,
		OnNewPeerHdlr:          broker.onNewPeer,
		OnPeerDisconnectedHdlr: broker.onPeerDisconnected,
		ExitOnCoordinatorClose: config.ExitOnCoordinatorClose,
		MaxPeers:               config.MaxPeers,
	})
	if err != nil {
		return nil, err
	}

	if broker.zipper == nil {
		broker.zipper = &GzipCompression{}
	}

	if broker.reliableWriterControllerFactory == nil {
		broker.reliableWriterControllerFactory = func(alias uint64, writer PeerWriter) WriterController {
			return NewBufferedWriterController(writer, 10, defaultMaxPeerBufferSize)
		}
	}

	if broker.unreliableWriterControllerFactory == nil {
		broker.unreliableWriterControllerFactory = func(alias uint64, writer PeerWriter) WriterController {
			return NewFixedQueueWriterController(writer, 10, defaultMaxPeerBufferSize)
		}
	}

	if broker.reliableChannelBufferedAmountLowThreshold == 0 {
		broker.reliableChannelBufferedAmountLowThreshold = defaultReliableChannelBufferedAmountLowThreshold
	}

	if broker.unreliableChannelBufferedAmountLowThreshold == 0 {
		broker.unreliableChannelBufferedAmountLowThreshold = defaultUnreliableChannelBufferedAmountLowThreshold
	}

	return broker, nil
}

// ProcessMessagesChannel start the topic message processor
func (b *Broker) ProcessMessagesChannel() {
	for {
		msg, ok := <-b.messagesCh
		if !ok {
			b.log.Info().Msg("exiting process message loop")
			return
		}

		b.processTopicMessage(msg)

		n := len(b.messagesCh)
		for i := 0; i < n; i++ {
			msg, ok = <-b.messagesCh
			if !ok {
				b.log.Info().Msg("exiting process message loop")
				return
			}

			b.processTopicMessage(msg)
		}
	}
}

// ProcessSubscriptionChannel start the subscription message processor
func (b *Broker) ProcessSubscriptionChannel() {
	ignoreError := func(err error) {
		if err != nil {
			b.log.Debug().Err(err).Msg("ignoring error")
		}
	}

	for {
		change, ok := <-b.subscriptionCh
		if !ok {
			b.log.Info().Msg("exiting process loop")
			return
		}

		ignoreError(b.processSubscriptionChange(change))

		n := len(b.subscriptionCh)
		for i := 0; i < n; i++ {
			change, ok := <-b.subscriptionCh
			if !ok {
				b.log.Info().Msg("exiting process loop")
				return
			}

			ignoreError(b.processSubscriptionChange(change))
		}
	}
}

func (b *Broker) processTopicMessage(msg *peerMessage) {
	topic := msg.topic
	fromServer := msg.fromServer
	reliable := msg.reliable

	b.subscriptionsLock.RLock()
	defer b.subscriptionsLock.RUnlock()

	subscription, ok := b.subscriptions[topic]
	if !ok {
		return
	}

	clientCount := uint32(0)

	for _, p := range subscription.clients {
		if p == msg.from {
			continue
		}

		clientCount++

		rawMsg := msg.rawMsgToClient

		if reliable {
			p.WriteReliable(rawMsg)
		} else {
			p.WriteUnreliable(rawMsg)
		}
	}

	var serverCount uint32

	if !fromServer || b.role == protocol.Role_COMMUNICATION_SERVER_HUB {
		for _, p := range subscription.servers {
			if p == msg.from {
				continue
			}
			serverCount++

			rawMsg := msg.rawMsgToServer

			if reliable {
				p.WriteReliable(rawMsg)
			} else {
				p.WriteUnreliable(rawMsg)
			}
		}
	}

	if verbose {
		b.log.Debug().
			Bool("reliable", reliable).
			Bool("fromServer", fromServer).
			Uint32("serverCount", serverCount).
			Uint32("clientCount", clientCount).
			Msg("broadcasting topic message")
	}
}

func (b *Broker) broadcastSubscriptionChange() error {
	b.subscriptionsLock.RLock()
	topics, err := b.subscriptions.buildTopicsBuffer()
	b.subscriptionsLock.RUnlock()

	if err != nil {
		return err
	}

	message := &protocol.SubscriptionMessage{
		Type:   protocol.MessageType_SUBSCRIPTION,
		Format: protocol.Format_PLAIN,
		Topics: topics,
	}

	rawMsg, err := proto.Marshal(message)
	if err != nil {
		b.log.Error().Err(err).Msg("encode topic subscription message failure")
		return err
	}

	var serverCount uint32

	for _, p := range b.peers {
		if p.getRole() == protocol.Role_COMMUNICATION_SERVER {
			p.Log.Debug().Msg("send topic change (to server)")

			serverCount++

			p.WriteReliable(rawMsg)
		}
	}

	if verbose {
		b.log.Debug().
			Uint64("serverAlias", b.Alias).
			Uint32("serverCount", serverCount).
			Str("topics", string(topics)).
			Msg("subscription message broadcasted")
	} else {
		b.log.Debug().
			Uint64("serverAlias", b.Alias).
			Uint32("serverCount", serverCount).
			Msg("subscription message broadcasted")
	}

	return nil
}

func (b *Broker) processSubscriptionChange(change subscriptionChange) error {
	p := change.peer
	role := p.getRole()
	topicsChanged := false
	rawTopics := change.rawTopics

	if change.format == protocol.Format_GZIP {
		unzipedTopics, err := b.zipper.Unzip(rawTopics)

		if err != nil {
			b.log.Error().Err(err).Msg("unzip failure")
			return err
		}

		rawTopics = unzipedTopics
	}

	topics := bytes.Split(rawTopics, []byte(" "))
	newTopics := make([]string, len(topics))

	if len(rawTopics) > 0 {
		// NOTE: check if topics were added
		for i, rawTopic := range topics {
			topic := string(rawTopic)

			newTopics[i] = topic

			if _, ok := p.topics[topic]; ok {
				continue
			}

			p.topics[topic] = struct{}{}

			b.subscriptionsLock.Lock()
			if role == protocol.Role_COMMUNICATION_SERVER {
				b.subscriptions.AddServerSubscription(topic, p)

				if b.role == protocol.Role_COMMUNICATION_SERVER_HUB {
					topicsChanged = true
				}
			} else if b.subscriptions.AddClientSubscription(topic, p) {
				topicsChanged = true
			}
			b.subscriptionsLock.Unlock()
		}
	}

	sort.Strings(newTopics)

	// NOTE: check if topics were deleted
	for topic := range p.topics {
		ix := sort.SearchStrings(newTopics, topic)
		if ix < len(newTopics) && newTopics[ix] == topic {
			continue
		}

		delete(p.topics, topic)

		b.subscriptionsLock.Lock()
		if role == protocol.Role_COMMUNICATION_SERVER {
			if b.subscriptions.RemoveServerSubscription(topic, p) &&
				b.role == protocol.Role_COMMUNICATION_SERVER_HUB {
				topicsChanged = true
			}
		} else if b.subscriptions.RemoveClientSubscription(topic, p) {
			topicsChanged = true
		}
		b.subscriptionsLock.Unlock()
	}

	if topicsChanged {
		return b.broadcastSubscriptionChange()
	}

	return nil
}

// GenerateCoordinatorConnectURL ...
func (b *Broker) GenerateCoordinatorConnectURL() (string, error) {
	url, err := b.auth.GenerateServerConnectURL(b.coordinatorURL, b.role)
	if err != nil {
		b.log.Error().Err(err).Msg("error generating communication server auth url")
		return "", err
	}

	return url, nil
}

// Connect connects the broker to the coordinator
func (b *Broker) Connect() error {
	connectURL, err := b.GenerateCoordinatorConnectURL()
	if err != nil {
		return err
	}

	welcomeMessage, err := b.ConnectCoordinator(connectURL)
	if err != nil {
		return err
	}

	for _, alias := range welcomeMessage.AvailableServers {
		b.initiatedConnections[alias] = protocol.Role_COMMUNICATION_SERVER
		if err := b.ConnectPeer(alias); err != nil {
			b.log.Error().Err(err).Msg("init peer error creating server (processing welcome)")
			return err
		}
	}

	return nil
}

// Shutdown ...
func (b *Broker) Shutdown() {
	server.Shutdown(b.Server)
}

// Stats ...
type Stats struct {
	Time                time.Time
	Alias               uint64
	ConnectChSize       int
	WebRtcControlChSize int
	UnregisterChSize    int
	TopicCount          int
	SubscriptionChSize  int
	MessagesChSize      int
	Peers               map[uint64]PeerStats
}

// PeerStats ...
type PeerStats struct {
	Alias      uint64
	Identity   []byte
	State      pion.ICEConnectionState
	TopicCount uint32

	Nomination          bool
	LocalCandidateType  pion.ICECandidateType
	RemoteCandidateType pion.ICECandidateType

	DataChannelsOpened    uint32
	DataChannelsClosed    uint32
	DataChannelsRequested uint32
	DataChannelsAccepted  uint32

	ReliableProtocol         string
	ReliableState            pion.DataChannelState
	ReliableBytesSent        uint64
	ReliableBytesReceived    uint64
	ReliableMessagesSent     uint32
	ReliableMessagesReceived uint32
	ReliableBufferedAmount   uint64

	UnreliableProtocol         string
	UnreliableState            pion.DataChannelState
	UnreliableBytesSent        uint64
	UnreliableBytesReceived    uint64
	UnreliableMessagesSent     uint32
	UnreliableMessagesReceived uint32
	UnreliableBufferedAmount   uint64

	ICETransportBytesSent     uint64
	ICETransportBytesReceived uint64

	SCTPTransportBytesSent     uint64
	SCTPTransportBytesReceived uint64
}

// GetBrokerStats ...
func (b *Broker) GetBrokerStats() Stats {
	getCandidateStats := func(report server.PeerStats, statsID string) (pion.ICECandidateStats, bool) {
		stats, ok := report.Get(statsID)
		if !ok {
			return pion.ICECandidateStats{}, ok
		}

		candidateStats, ok := stats.(pion.ICECandidateStats)
		if !ok {
			b.log.Warn().Msgf("requested ice candidate type %s, but is not from type ICECandidateStats", statsID)
		}

		return candidateStats, ok
	}

	getTransportStats := func(report server.PeerStats, statsID string) (pion.TransportStats, bool) {
		stats, ok := report.Get(statsID)
		if !ok {
			return pion.TransportStats{}, ok
		}

		transportStats, ok := stats.(pion.TransportStats)
		if !ok {
			b.log.Warn().Msgf("requested transport stats type %s, but is not from type TransportStats", statsID)
		}

		return transportStats, ok
	}

	b.subscriptionsLock.RLock()
	topicCount := len(b.subscriptions)
	b.subscriptionsLock.RUnlock()

	serverStats := b.GetServerStats()

	brokerStats := Stats{
		Time:                serverStats.Time,
		Alias:               serverStats.Alias,
		Peers:               make(map[uint64]PeerStats, len(serverStats.Peers)),
		ConnectChSize:       serverStats.ConnectChSize,
		WebRtcControlChSize: serverStats.WebRtcControlChSize,
		UnregisterChSize:    serverStats.UnregisterChSize,
		TopicCount:          topicCount,
		SubscriptionChSize:  len(b.subscriptionCh),
		MessagesChSize:      len(b.messagesCh),
	}

	for _, report := range serverStats.Peers {
		b.peersMux.Lock()
		p := b.peers[report.Alias]
		b.peersMux.Unlock()

		stats := PeerStats{
			Alias:      p.Alias,
			Identity:   p.GetIdentity(),
			TopicCount: uint32(len(p.topics)),
			State:      p.Conn.ICEConnectionState(),
		}

		if p.reliableDC != nil {
			stats.ReliableBufferedAmount = p.reliableDC.BufferedAmount()
		}

		if p.unreliableDC != nil {
			stats.UnreliableBufferedAmount = p.unreliableDC.BufferedAmount()
		}

		connStats, ok := report.GetConnectionStats(p.Conn)
		if ok {
			stats.DataChannelsOpened = connStats.DataChannelsOpened
			stats.DataChannelsClosed = connStats.DataChannelsClosed
			stats.DataChannelsRequested = connStats.DataChannelsRequested
			stats.DataChannelsAccepted = connStats.DataChannelsAccepted
		}

		reliableStats, ok := report.GetDataChannelStats(p.reliableDC)
		if ok {
			stats.ReliableProtocol = reliableStats.Protocol
			stats.ReliableState = reliableStats.State
			stats.ReliableMessagesSent = reliableStats.MessagesSent
			stats.ReliableBytesSent = reliableStats.BytesSent
			stats.ReliableMessagesReceived = reliableStats.MessagesReceived
			stats.ReliableBytesReceived = reliableStats.BytesReceived
		}

		unreliableStats, ok := report.GetDataChannelStats(p.unreliableDC)
		if ok {
			stats.UnreliableProtocol = unreliableStats.Protocol
			stats.UnreliableState = unreliableStats.State
			stats.UnreliableMessagesSent = unreliableStats.MessagesSent
			stats.UnreliableBytesSent = unreliableStats.BytesSent
			stats.UnreliableMessagesReceived = unreliableStats.MessagesReceived
			stats.UnreliableBytesReceived = unreliableStats.BytesReceived
		}

		iceTransportStats, ok := getTransportStats(report, "iceTransport")
		if ok {
			stats.ICETransportBytesSent = iceTransportStats.BytesSent
			stats.ICETransportBytesReceived = iceTransportStats.BytesReceived
		}

		sctpTransportStats, ok := getTransportStats(report, "sctpTransport")
		if ok {
			stats.SCTPTransportBytesSent = sctpTransportStats.BytesSent
			stats.SCTPTransportBytesReceived = sctpTransportStats.BytesReceived
		}

		for _, v := range report.StatsReport {
			pairStats, ok := v.(pion.ICECandidatePairStats)

			if !ok || !pairStats.Nominated {
				continue
			}

			stats.Nomination = true

			localCandidateStats, ok := getCandidateStats(report, pairStats.LocalCandidateID)
			if ok {
				stats.LocalCandidateType = localCandidateStats.CandidateType
			}

			remoteCandidateStats, ok := getCandidateStats(report, pairStats.RemoteCandidateID)
			if ok {
				stats.RemoteCandidateType = remoteCandidateStats.CandidateType
			}
		}

		brokerStats.Peers[report.Alias] = stats
	}

	return brokerStats
}

func (b *Broker) onNewPeer(rawPeer *server.Peer) error {
	role := protocol.Role_UNKNOWN_ROLE

	if knownRole, ok := b.initiatedConnections[rawPeer.Alias]; ok {
		role = knownRole

		delete(b.initiatedConnections, rawPeer.Alias)
	}

	var err error

	p := &peer{
		Peer:           rawPeer,
		topics:         make(map[string]struct{}),
		subscriptionCh: b.subscriptionCh,
		messagesCh:     b.messagesCh,
		role:           int32(role),
	}

	p.reliableDC, err = p.Conn.CreateDataChannel("reliable", nil)
	if err != nil {
		p.Log.Error().Err(err).Msg("cannot create new reliable data channel")

		if err = p.Conn.Close(); err != nil {
			p.Log.Debug().Err(err).Msg("error closing connection")
			return err
		}

		return err
	}

	p.reliableDC.SetBufferedAmountLowThreshold(b.reliableChannelBufferedAmountLowThreshold)

	maxRetransmits := uint16(0)
	ordered := false
	options := &pion.DataChannelInit{
		MaxRetransmits: &maxRetransmits,
		Ordered:        &ordered,
	}

	p.unreliableDC, err = p.Conn.CreateDataChannel("unreliable", options)
	if err != nil {
		p.Log.Error().Err(err).Msg("cannot create new unreliable data channel")

		if err = p.Conn.Close(); err != nil {
			p.Log.Debug().Err(err).Msg("error closing connection")
			return err
		}

		return err
	}

	p.unreliableDC.SetBufferedAmountLowThreshold(b.unreliableChannelBufferedAmountLowThreshold)

	unreliableDCReady := make(chan bool)

	p.reliableWriter = b.reliableWriterControllerFactory(p.Alias, &reliablePeerWriter{p})
	p.reliableDC.OnBufferedAmountLow(p.reliableWriter.OnBufferedAmountLow)

	p.unreliableWriter = b.unreliableWriterControllerFactory(p.Alias, &unreliablePeerWriter{p})
	p.unreliableDC.OnBufferedAmountLow(p.unreliableWriter.OnBufferedAmountLow)

	b.peersMux.Lock()
	b.peers[p.Alias] = p
	b.peersMux.Unlock()

	p.reliableDC.OnOpen(func() {
		p.Log.Info().Msg("Reliable data channel open")
		d, err := p.reliableDC.Detach()
		if err != nil {
			p.Log.Error().Err(err).Msg("cannot detach data channel")
			p.Close()
			return
		}

		p.reliableRWCMutex.Lock()
		p.reliableRWC = d
		p.reliableRWCMutex.Unlock()

		if p.getRole() == protocol.Role_UNKNOWN_ROLE {
			p.Log.Debug().Msg("unknown role, waiting for auth message")
			header := protocol.MessageHeader{}
			buffer := make([]byte, maxWorldCommMessageSize)
			n, err := d.Read(buffer)

			if err != nil {
				p.Log.Error().Err(err).Msg("datachannel closed before auth")
				p.Close()
				return
			}

			rawMsg := buffer[:n]
			if err = proto.Unmarshal(rawMsg, &header); err != nil {
				p.Log.Error().Err(err).Msg("decode auth header message failure")
				p.Close()
				return
			}

			msgType := header.GetType()

			if msgType != protocol.MessageType_AUTH {
				p.Log.Info().Str("msgType", msgType.String()).
					Msg("closing connection: sending data without authorization")
				p.Close()
				return
			}

			authMessage := protocol.AuthMessage{}
			if err = proto.Unmarshal(rawMsg, &authMessage); err != nil {
				p.Log.Error().Err(err).Msg("decode auth message failure")
				p.Close()
				return
			}

			if authMessage.Role == protocol.Role_UNKNOWN_ROLE {
				p.Log.Error().Err(err).Msg("unknown role")
				p.Close()
				return
			}

			isValid, identity, err := b.auth.AuthenticateFromMessage(authMessage.Role, authMessage.Body)
			if err != nil {
				p.Log.Error().Err(err).Msg("authentication error")
				p.Close()
				return
			}

			if isValid {
				atomic.StoreInt32(&p.role, int32(authMessage.Role))
				p.identity.Store(identity)
				p.Log.Debug().Msg("peer authorized")

				if authMessage.Role == protocol.Role_COMMUNICATION_SERVER {
					b.subscriptionsLock.Lock()
					topics, err := b.subscriptions.buildTopicsBuffer()
					b.subscriptionsLock.Unlock()
					if err != nil {
						p.Log.Error().Err(err).Msg("build topic buffer error")
						p.Close()
						return
					}

					if len(topics) > 0 {
						topicSubscriptionMessage := &protocol.SubscriptionMessage{
							Type:   protocol.MessageType_SUBSCRIPTION,
							Format: protocol.Format_PLAIN,
							Topics: topics,
						}

						rawMsg, err := proto.Marshal(topicSubscriptionMessage)
						if err != nil {
							p.Log.Error().Err(err).Msg("encode topic subscription message failure")
							p.Close()
							return
						}

						p.WriteReliable(rawMsg)
					}
				}
			} else {
				p.Log.Info().Msg("closing connection: not authorized")
				p.Close()
				return
			}
		} else {
			p.Log.Debug().Msg("role already identified, sending auth message")
			authMessage, err := b.auth.GenerateServerAuthMessage()

			if err != nil {
				p.Log.Error().Err(err).Msg("cannot create auth message")
				p.Close()
				return
			}

			rawMsg, err := proto.Marshal(authMessage)
			if err != nil {
				p.Log.Error().Err(err).Msg("cannot encode auth message")
				p.Close()
				return
			}

			if _, err := d.Write(rawMsg); err != nil {
				p.Log.Error().Err(err).Msg("error writing message")
				p.Close()
				return
			}
		}

		go p.readReliablePump()

		<-unreliableDCReady
		go p.readUnreliablePump()
	})

	p.unreliableDC.OnOpen(func() {
		p.Log.Info().Msg("Unreliable data channel open")
		d, err := p.unreliableDC.Detach()
		if err != nil {
			p.Log.Error().Err(err).Msg("cannot detach datachannel")
			p.Close()
			return
		}

		p.unreliableRWCMutex.Lock()
		p.unreliableRWC = d
		p.unreliableRWCMutex.Unlock()
		unreliableDCReady <- true
	})

	return nil
}

func (b *Broker) onPeerDisconnected(rawPeer *server.Peer) {
	b.peersMux.Lock()

	p, ok := b.peers[rawPeer.Alias]
	if ok {
		delete(b.peers, rawPeer.Alias)
	}

	b.peersMux.Unlock()

	if !ok {
		return
	}

	topicsChanged := false

	b.subscriptionsLock.Lock()
	if p.getRole() == protocol.Role_COMMUNICATION_SERVER {
		for topic := range p.topics {
			if b.subscriptions.RemoveServerSubscription(topic, p) &&
				b.role == protocol.Role_COMMUNICATION_SERVER_HUB {
				topicsChanged = true
			}
		}
	} else {
		for topic := range p.topics {
			if b.subscriptions.RemoveClientSubscription(topic, p) {
				topicsChanged = true
			}
		}
	}
	b.subscriptionsLock.Unlock()

	if topicsChanged {
		if err := b.broadcastSubscriptionChange(); err != nil {
			b.log.Error().Err(err).Msg("cannot broadcast subscription change on peer disconnected")
		}

		return
	}
}
