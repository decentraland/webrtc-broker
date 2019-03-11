package worldcomm

import (
	"bytes"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/utils"
	"github.com/decentraland/communications-server-go/internal/webrtc"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
)

const (
	maxWorldCommMessageSize = 5120
	logTopicMessageReceived = false
	logTopicSubscription    = false
)

type topicChange struct {
	peer      *peer
	format    protocol.Format
	rawTopics []byte
}

type peerMessage struct {
	fromServer bool
	reliable   bool
	topic      string
	from       *peer
	rawMsg     []byte
}

type peer struct {
	isServer           bool
	authenticationSent bool
	Alias              string
	sendReliable       chan []byte
	sendUnreliable     chan []byte
	conn               webrtc.IWebRtcConnection

	isAuthenticatedMux sync.Mutex
	isAuthenticated    bool

	isClosedMux sync.RWMutex
	isClosed    bool

	Topics map[string]struct{}
}

func (p *peer) hasTopic(topic string) bool {
	_, ok := p.Topics[topic]
	return ok
}

func (p *peer) IsAuthenticated() bool {
	p.isAuthenticatedMux.Lock()
	defer p.isAuthenticatedMux.Unlock()
	return p.isAuthenticated
}

func (p *peer) SetIsAuthenticated(isAuthenticated bool) {
	p.isAuthenticatedMux.Lock()
	defer p.isAuthenticatedMux.Unlock()
	p.isAuthenticated = isAuthenticated
}

type topicSubscription struct {
	clients []*peer
	servers []*peer
}

func (s *topicSubscription) isEmpty() bool {
	return len(s.clients) == 0 && len(s.servers) == 0
}

type topicSubscriptions map[string]*topicSubscription

func (ts *topicSubscriptions) AddClientSubscription(topic string, client *peer) (subscriptionChanged bool) {
	s, ok := (*ts)[topic]

	if ok {
		s.clients = append(s.clients, client)
		return false
	}

	s = &topicSubscription{
		clients: make([]*peer, 1),
		servers: make([]*peer, 0),
	}

	s.clients[0] = client
	(*ts)[topic] = s

	return true
}

func (ts *topicSubscriptions) AddServerSubscription(topic string, server *peer) (subscriptionChanged bool) {
	s, ok := (*ts)[topic]

	if ok {
		s.servers = append(s.servers, server)
		return false
	}

	s = &topicSubscription{
		clients: make([]*peer, 0),
		servers: make([]*peer, 1),
	}

	s.servers[0] = server
	(*ts)[topic] = s

	return true
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
	} else {
		return false
	}
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
	} else {
		return false
	}
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

func findPeer(peers []*peer, alias string) *peer {
	for _, p := range peers {
		if p.Alias == alias {
			return p
		}
	}

	return nil
}

type WorldCommunicationState struct {
	// NOTE: dependencies
	Auth       authentication.Authentication
	marshaller protocol.IMarshaller
	log        *logging.Logger
	agent      worldCommAgent
	webRtc     webrtc.IWebRtc
	Reporter   IReporter
	zipper     utils.ZipCompression

	authMethod            string
	coordinator           *Coordinator
	Peers                 []*peer
	topicQueue            chan topicChange
	connectQueue          chan string
	webRtcControlQueue    chan *protocol.WebRtcMessage
	messagesQueue         chan *peerMessage
	unregisterQueue       chan *peer
	serverRegisteredQueue chan *peer
	aliasChannel          chan string
	stop                  chan bool
	softStop              bool
	ReportPeriod          time.Duration

	alias    string
	aliasMux sync.RWMutex

	subscriptions topicSubscriptions
}

func (s *WorldCommunicationState) GetAlias() string {
	s.aliasMux.RLock()
	defer s.aliasMux.RUnlock()
	return s.alias
}

type IReporter interface {
	Report(state *WorldCommunicationState)
}

type defaultReporter struct{}

func (r *defaultReporter) Report(state *WorldCommunicationState) {
	peersCount := len(state.Peers)
	state.agent.RecordTotalPeerConnections(peersCount)
	state.agent.RecordTotalTopicSubscriptions(peersCount)

	state.log.WithFields(logging.Fields{
		"peers count":  peersCount,
		"topics count": len(state.subscriptions),
	}).Info("report")

	state.log.WithFields(logging.Fields{
		"topicQ_size":            len(state.topicQueue),
		"connectQ_size":          len(state.connectQueue),
		"webrtc_controlQ_size":   len(state.webRtcControlQueue),
		"messagesQ_size":         len(state.messagesQueue),
		"unregisterQ_size":       len(state.unregisterQueue),
		"serverRegisteredQ_size": len(state.serverRegisteredQueue),
	}).Info("queue status")

}

func MakeState(agent agent.IAgent, authMethod string, coordinatorUrl string) WorldCommunicationState {
	return WorldCommunicationState{
		authMethod:            authMethod,
		Auth:                  authentication.Make(),
		marshaller:            &protocol.Marshaller{},
		log:                   logging.New(),
		webRtc:                webrtc.MakeWebRtc(),
		agent:                 worldCommAgent{agent: agent},
		coordinator:           makeCoordinator(coordinatorUrl),
		zipper:                &utils.GzipCompression{},
		Peers:                 make([]*peer, 0),
		subscriptions:         make(topicSubscriptions),
		stop:                  make(chan bool),
		unregisterQueue:       make(chan *peer, 255),
		serverRegisteredQueue: make(chan *peer, 255),
		aliasChannel:          make(chan string),
		topicQueue:            make(chan topicChange, 255),
		connectQueue:          make(chan string, 255),
		messagesQueue:         make(chan *peerMessage, 255),
		webRtcControlQueue:    make(chan *protocol.WebRtcMessage, 255),
		Reporter:              &defaultReporter{},
		ReportPeriod:          30 * time.Second,
	}
}

func (p *peer) IsClosed() bool {
	p.isClosedMux.RLock()
	defer p.isClosedMux.RUnlock()
	return p.isClosed
}

func (p *peer) Close() {
	p.isClosedMux.Lock()
	defer p.isClosedMux.Unlock()
	if !p.isClosed {
		if p.conn != nil {
			p.conn.Close()
		}
		p.isClosed = true
	}
}

func readPeerMessage(state *WorldCommunicationState, p *peer, reliable bool, rawMsg []byte, topicMessage *protocol.TopicMessage) {
	log := state.log
	marshaller := state.marshaller

	if err := marshaller.Unmarshal(rawMsg, topicMessage); err != nil {
		log.WithError(err).Debug("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		log.WithFields(logging.Fields{
			"peer":     p.Alias,
			"reliable": reliable,
			"topic":    topicMessage.Topic,
		}).Debug("message received")
	}

	if p.isServer {
		msg := &peerMessage{
			fromServer: true,
			reliable:   reliable,
			topic:      topicMessage.Topic,
			from:       p,
			rawMsg:     rawMsg,
		}

		state.messagesQueue <- msg
	} else {
		topicMessage.FromAlias = p.Alias
		rawMsg, err := marshaller.Marshal(topicMessage)
		if err != nil {
			log.WithError(err).Error("encode topic message failure")
			return
		}

		msg := &peerMessage{
			fromServer: false,
			reliable:   reliable,
			topic:      topicMessage.Topic,
			from:       p,
			rawMsg:     rawMsg,
		}

		state.messagesQueue <- msg
	}
}

func (p *peer) readReliablePump(state *WorldCommunicationState) {
	marshaller := state.marshaller
	auth := state.Auth
	log := state.log
	header := &protocol.WorldCommMessage{}
	topicMessage := &protocol.TopicMessage{}

	buffer := make([]byte, maxWorldCommMessageSize)

	initialized := false

	for {
		n, err := p.conn.ReadReliable(buffer)

		if err != nil {
			log.WithError(err).WithFields(logging.Fields{
				"id":   state.GetAlias(),
				"peer": p.Alias,
			}).Info("exit peer.readReliablePump(), datachannel closed")

			p.Close()
			state.unregisterQueue <- p
			return
		}

		if n == 0 {
			continue
		}

		state.agent.RecordReceivedReliableFromPeerSize(n)
		rawMsg := buffer[:n]
		if err := marshaller.Unmarshal(rawMsg, header); err != nil {
			log.WithField("peer", p.Alias).WithError(err).Debug("decode header message failure")
			continue
		}

		msgType := header.GetType()

		if !initialized {
			initialized = true

			if !p.IsAuthenticated() && msgType != protocol.MessageType_AUTH {
				log.WithField("peer", p.Alias).Debug("closing connection: sending data without authorization")
				p.Close()
				break
			}
		}

		switch msgType {
		case protocol.MessageType_AUTH:
			authMessage := &protocol.AuthMessage{}
			if err := marshaller.Unmarshal(rawMsg, authMessage); err != nil {
				log.WithField("peer", p.Alias).WithError(err).Debug("decode auth message failure")
				continue
			}

			if authMessage.Role == protocol.Role_UNKNOWN_ROLE {
				log.WithField("peer", p.Alias).WithError(err).Debug("unknown role")
				continue
			}

			isValid, err := auth.Authenticate(authMessage.Method, authMessage.Role, authMessage.Body)
			if err != nil {
				log.WithField("peer", p.Alias).WithError(err).Error("authentication error")
				p.Close()
				break
			}

			if isValid {
				p.isServer = authMessage.Role == protocol.Role_COMMUNICATION_SERVER
				p.SetIsAuthenticated(true)

				log.WithFields(logging.Fields{
					"id":       state.GetAlias(),
					"peer":     p.Alias,
					"isServer": p.isServer,
				}).Debug("peer authorized")
				if p.isServer {
					if !p.authenticationSent {
						authMessage, err := state.Auth.GenerateAuthMessage(state.authMethod,
							protocol.Role_COMMUNICATION_SERVER)

						if err != nil {
							log.WithError(err).Error("cannot create auth message")
							continue
						}

						rawMsg, err := state.marshaller.Marshal(authMessage)
						if err != nil {
							log.WithError(err).Error("cannot encode auth message")
							return
						}

						p.sendReliable <- rawMsg
					}

					state.serverRegisteredQueue <- p
				}
			} else {
				log.WithField("peer", p.Alias).Debug("closing connection: not authorized")
				p.Close()
				break
			}
		case protocol.MessageType_TOPIC_SUBSCRIPTION:
			topicSubscriptionMessage := &protocol.TopicSubscriptionMessage{}
			if err := marshaller.Unmarshal(rawMsg, topicSubscriptionMessage); err != nil {
				log.WithField("peer", p.Alias).WithError(err).Debug("decode add topic message failure")
				continue
			}

			state.topicQueue <- topicChange{
				peer:      p,
				format:    topicSubscriptionMessage.Format,
				rawTopics: topicSubscriptionMessage.Topics,
			}
		case protocol.MessageType_TOPIC:
			readPeerMessage(state, p, true, rawMsg, topicMessage)
		case protocol.MessageType_PING:
			p.sendUnreliable <- rawMsg
		default:
			log.WithField("type", msgType).Debug("unhandled reliable message from peer")
		}
	}

}

func (p *peer) readUnreliablePump(state *WorldCommunicationState) {
	marshaller := state.marshaller
	log := state.log

	header := &protocol.WorldCommMessage{}
	topicMessage := &protocol.TopicMessage{}

	buffer := make([]byte, maxWorldCommMessageSize)
	initialized := false
	for {
		n, err := p.conn.ReadUnreliable(buffer)

		if err != nil {
			log.WithError(err).WithFields(logging.Fields{
				"id":   state.GetAlias(),
				"peer": p.Alias,
			}).Info("exit peer.readUnreliablePump(), datachannel closed")

			p.Close()
			return
		}

		if n == 0 {
			continue
		}

		state.agent.RecordReceivedUnreliableFromPeerSize(n)
		rawMsg := buffer[:n]
		if err := marshaller.Unmarshal(rawMsg, header); err != nil {
			log.WithField("peer", p.Alias).WithError(err).Debug("decode header message failure")
			continue
		}

		msgType := header.GetType()

		if initialized {
			initialized = true
			if !p.IsAuthenticated() {
				log.WithField("peer", p.Alias).Debug("closing connection: sending data without authorization")
				p.Close()
				return
			}
		}

		switch msgType {
		case protocol.MessageType_TOPIC:
			readPeerMessage(state, p, false, rawMsg, topicMessage)
		case protocol.MessageType_PING:
			p.sendUnreliable <- rawMsg
		default:
			log.WithField("type", msgType).Debug("unhandled unreliable message from peer")
		}
	}
}

func (p *peer) writePump(state *WorldCommunicationState) {
	defer func() {
		p.Close()
	}()
	log := state.log
	for {
		select {
		case rawMsg, ok := <-p.sendReliable:
			if !ok {
				log.WithFields(logging.Fields{
					"id":   state.GetAlias(),
					"peer": p.Alias,
				}).Info("exit peer.writePump(), sendReliable channel closed")
				return
			}

			if err := p.conn.WriteReliable(rawMsg); err != nil {
				log.WithField("peer", p.Alias).WithError(err).Error("error writing message")
				return
			}

			state.agent.RecordSentReliableToPeerSize(len(rawMsg))
			n := len(p.sendReliable)
			for i := 0; i < n; i++ {
				rawMsg = <-p.sendReliable
				if err := p.conn.WriteReliable(rawMsg); err != nil {
					log.WithField("peer", p.Alias).WithError(err).Error("error writing message")
					return
				}
				state.agent.RecordSentReliableToPeerSize(len(rawMsg))
			}
		case rawMsg, ok := <-p.sendUnreliable:
			if !ok {
				log.WithFields(logging.Fields{
					"id":   state.GetAlias(),
					"peer": p.Alias,
				}).Info("exit peer.writePump(), sendUnreliable channel closed")
				return
			}

			if err := p.conn.WriteUnreliable(rawMsg); err != nil {
				log.WithField("peer", p.Alias).WithError(err).Error("error writing message")
				return
			}

			state.agent.RecordSentUnreliableToPeerSize(len(rawMsg))
			n := len(p.sendUnreliable)
			for i := 0; i < n; i++ {
				rawMsg = <-p.sendUnreliable
				if err := p.conn.WriteUnreliable(rawMsg); err != nil {
					log.WithField("peer", p.Alias).WithError(err).Error("error writing message")
					return
				}
				state.agent.RecordSentUnreliableToPeerSize(len(rawMsg))
			}
		}
	}
}

func ConnectCoordinator(state *WorldCommunicationState) error {
	c := state.coordinator
	if err := c.Connect(state, state.authMethod); err != nil {
		return err
	}

	go c.readPump(state)
	go c.writePump(state)

	state.aliasMux.Lock()
	state.alias = <-state.aliasChannel
	state.aliasMux.Unlock()

	return nil
}

func closeState(state *WorldCommunicationState) {
	state.coordinator.Close()
	close(state.webRtcControlQueue)
	close(state.connectQueue)
	close(state.topicQueue)
	close(state.messagesQueue)
	close(state.unregisterQueue)
	close(state.stop)
}

func Process(state *WorldCommunicationState) {
	ticker := time.NewTicker(state.ReportPeriod)
	defer ticker.Stop()

	log := state.log
	for {
		select {
		case alias := <-state.connectQueue:
			processConnect(state, alias)
			n := len(state.connectQueue)
			for i := 0; i < n; i++ {
				alias := <-state.connectQueue
				processConnect(state, alias)
			}
		case change := <-state.topicQueue:
			ProcessTopicChange(state, change)
			n := len(state.topicQueue)
			for i := 0; i < n; i++ {
				change := <-state.topicQueue
				ProcessTopicChange(state, change)
			}
		case p := <-state.unregisterQueue:
			processUnregister(state, p)
			n := len(state.unregisterQueue)
			for i := 0; i < n; i++ {
				p = <-state.unregisterQueue
				processUnregister(state, p)
			}
		case webRtcMessage := <-state.webRtcControlQueue:
			processWebRtcControlMessage(state, webRtcMessage)
			n := len(state.webRtcControlQueue)
			for i := 0; i < n; i++ {
				webRtcMessage = <-state.webRtcControlQueue
				processWebRtcControlMessage(state, webRtcMessage)
			}
		case msg := <-state.messagesQueue:
			processPeerMessage(state, msg)
			n := len(state.messagesQueue)
			for i := 0; i < n; i++ {
				msg = <-state.messagesQueue
				processPeerMessage(state, msg)
			}
		case p := <-state.serverRegisteredQueue:
			buffer := bytes.Buffer{}
			i := 0
			last := len(state.subscriptions) - 1
			for topic := range state.subscriptions {
				buffer.WriteString(topic)
				if i != last {
					buffer.WriteString(" ")
				}
				i += 1
			}

			zipped, err := state.zipper.Zip(buffer.Bytes())
			if err != nil {
				log.WithError(err).Error("zip failure")
				break
			}
			topicSubscriptionMessage := &protocol.TopicSubscriptionMessage{
				Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
				Format: protocol.Format_GZIP,
			}
			topicSubscriptionMessage.Topics = zipped

			rawMsg, err := state.marshaller.Marshal(topicSubscriptionMessage)
			if err != nil {
				log.WithError(err).Error("encode add topic message failure")
				break
			}

			p.sendReliable <- rawMsg

			n := len(state.serverRegisteredQueue)
			for i := 0; i < n; i++ {
				p = <-state.serverRegisteredQueue
				p.sendReliable <- rawMsg
			}
		case <-ticker.C:
			state.Reporter.Report(state)
		case <-state.stop:
			log.Debug("hard stop signal")
			return
		}

		// NOTE: I'm using this for testing only, but if it makes sense to fully support it
		// we may want to add a timeout (with a timer), otherwise this will executed only
		// if the previous select exited
		if state.softStop {
			log.Debug("soft stop signal")
			return
		}
	}
}

func initPeer(state *WorldCommunicationState, alias string) (*peer, error) {
	log := state.log
	log.WithFields(logging.Fields{
		"id":   state.GetAlias(),
		"peer": alias,
	}).Debug("init peer")
	conn, err := state.webRtc.NewConnection()

	if err != nil {
		log.WithError(err).Error("error creating new peer connection")
		return nil, err
	}

	p := &peer{
		sendReliable:   make(chan []byte, 256),
		sendUnreliable: make(chan []byte, 256),
		Alias:          alias,
		conn:           conn,
		Topics:         make(map[string]struct{}),
	}

	state.Peers = append(state.Peers, p)

	conn.OnReliableChannelOpen(func() {
		go p.writePump(state)
		go p.readReliablePump(state)
	})

	conn.OnUnreliableChannelOpen(func() {
		go p.readUnreliablePump(state)
	})

	return p, nil
}

func processUnregister(state *WorldCommunicationState, p *peer) {
	log := state.log

	alias := p.Alias
	log.WithField("peer", alias).Debug("unregister peer")

	state.Peers = removePeer(state.Peers, p)

	close(p.sendReliable)
	close(p.sendUnreliable)

	if p.isServer {
		for topic := range p.Topics {
			state.subscriptions.RemoveServerSubscription(topic, p)
		}
	} else {
		topicsChanged := false

		for topic := range p.Topics {
			if state.subscriptions.RemoveClientSubscription(topic, p) {
				topicsChanged = true
			}
		}

		if topicsChanged {
			broadcastTopicChange(state)
		}
	}
}

func processConnect(state *WorldCommunicationState, alias string) error {
	log := state.log

	oldP := findPeer(state.Peers, alias)
	if oldP != nil && !oldP.IsClosed() {
		processUnregister(state, oldP)
	}

	p, err := initPeer(state, alias)
	if err != nil {
		return err
	}

	offer, err := p.conn.CreateOffer()
	if err != nil {
		log.WithField("peer", alias).WithError(err).Error("cannot create offer")
		return err
	}

	state.coordinator.Send(state, &protocol.WebRtcMessage{
		Type:    protocol.MessageType_WEBRTC_OFFER,
		Sdp:     offer,
		ToAlias: alias,
	})
	return nil
}

func ProcessTopicChange(state *WorldCommunicationState, change topicChange) error {
	log := state.log
	p := change.peer

	topicsChanged := false

	rawTopics := change.rawTopics
	if change.format == protocol.Format_GZIP {
		unzipedTopics, err := state.zipper.Unzip(rawTopics)

		if err != nil {
			log.WithError(err).Error("unzip failure")
			return err
		}

		rawTopics = unzipedTopics
	}

	newTopics := make([]string, len(rawTopics))

	if len(rawTopics) > 0 {
		// NOTE: check if topics were added
		for i, rawTopic := range bytes.Split(rawTopics, []byte(" ")) {
			topic := string(rawTopic)

			newTopics[i] = topic

			if _, ok := p.Topics[topic]; ok {
				continue
			}

			p.Topics[topic] = struct{}{}

			if p.isServer {
				state.subscriptions.AddServerSubscription(topic, p)
			} else {
				if state.subscriptions.AddClientSubscription(topic, p) {
					topicsChanged = true
				}
			}
		}
	}

	sort.Strings(newTopics)

	// NOTE: check if topics were deleted
	for topic := range p.Topics {
		if sort.SearchStrings(newTopics, topic) < len(newTopics) {
			continue
		}

		delete(p.Topics, topic)

		if p.isServer {
			state.subscriptions.RemoveServerSubscription(topic, p)
		} else {
			if state.subscriptions.RemoveClientSubscription(topic, p) {
				topicsChanged = true
			}
		}
	}

	if topicsChanged {
		return broadcastTopicChange(state)
	}

	return nil
}

func processWebRtcControlMessage(state *WorldCommunicationState, webRtcMessage *protocol.WebRtcMessage) error {
	log := state.log

	alias := webRtcMessage.FromAlias
	p := findPeer(state.Peers, alias)
	if p == nil {
		np, err := initPeer(state, alias)
		if err != nil {
			return err
		}
		p = np
	}

	switch webRtcMessage.Type {
	case protocol.MessageType_WEBRTC_OFFER:
		log.WithFields(logging.Fields{
			"id":   state.GetAlias(),
			"peer": p.Alias,
		}).Debug("webrtc offer received")
		answer, err := p.conn.OnOffer(webRtcMessage.Sdp)
		if err != nil {
			log.WithError(err).Error("error setting webrtc offer")
			return err
		}

		state.coordinator.Send(state, &protocol.WebRtcMessage{
			Type:    protocol.MessageType_WEBRTC_ANSWER,
			Sdp:     answer,
			ToAlias: p.Alias,
		})
	case protocol.MessageType_WEBRTC_ANSWER:
		log.WithFields(logging.Fields{
			"id":   state.GetAlias(),
			"peer": p.Alias,
		}).Debug("webrtc answer received")
		if err := p.conn.OnAnswer(webRtcMessage.Sdp); err != nil {
			log.WithError(err).Error("error setting webrtc answer")
			return err
		}
	case protocol.MessageType_WEBRTC_ICE_CANDIDATE:
		log.WithFields(logging.Fields{
			"id":   state.GetAlias(),
			"peer": p.Alias,
		}).Debug("ice candidate received")
		if err := p.conn.OnIceCandidate(webRtcMessage.Sdp); err != nil {
			log.WithError(err).Error("error adding remote ice candidate")
			return err
		}
	default:
		log.Fatal(errors.New("invalid message type in processWebRtcControlMessage"))
	}

	return nil
}

func processPeerMessage(state *WorldCommunicationState, msg *peerMessage) {
	topic := msg.topic
	fromServer := msg.fromServer
	reliable := msg.reliable

	subscription, ok := state.subscriptions[topic]

	if !ok {
		return
	}

	for _, p := range subscription.clients {
		if p == msg.from || p.IsClosed() {
			continue
		}
		if reliable {
			p.sendReliable <- msg.rawMsg
		} else {
			p.sendUnreliable <- msg.rawMsg
		}
	}

	if !fromServer {
		for _, p := range subscription.servers {
			if p.IsClosed() {
				continue
			}
			if reliable {
				p.sendReliable <- msg.rawMsg
			} else {
				p.sendUnreliable <- msg.rawMsg
			}
		}
	}
}

func broadcastTopicChange(state *WorldCommunicationState) error {
	log := state.log

	buffer := bytes.Buffer{}
	i := 0
	last := len(state.subscriptions) - 1
	for topic := range state.subscriptions {
		buffer.WriteString(topic)
		if i != last {
			buffer.WriteString(" ")
		}
		i += 1
	}

	encodedTopics, err := state.zipper.Zip(buffer.Bytes())
	if err != nil {
		return err
	}

	message := &protocol.TopicSubscriptionMessage{
		Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
		Format: protocol.Format_GZIP,
		Topics: encodedTopics,
	}

	rawMsg, err := state.marshaller.Marshal(message)
	if err != nil {
		log.WithError(err).Error("encode topic subscription message failure")
		return err
	}

	for _, p := range state.Peers {
		if p.isServer {
			log.WithFields(logging.Fields{
				"id":   state.GetAlias(),
				"peer": p.Alias,
			}).Debug("send topic change (to server)")
			p.sendReliable <- rawMsg
		}
	}

	return nil
}
