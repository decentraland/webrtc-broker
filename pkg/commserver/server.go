package commserver

import (
	"bytes"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/internal/webrtc"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/sirupsen/logrus"

	pion "github.com/pion/webrtc/v2"
)

const (
	maxWorldCommMessageSize = 5120
	logTopicMessageReceived = false
)

type topicChange struct {
	peer      *peer
	format    protocol.Format
	rawTopics []byte
}

type peerMessage struct {
	receivedAt     time.Time
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

func findPeer(peers []*peer, alias uint64) *peer {
	for _, p := range peers {
		if p.Alias == alias {
			return p
		}
	}

	return nil
}

type services struct {
	Auth       authentication.Authentication
	Log        *logging.Logger
	Marshaller protocol.IMarshaller
	WebRtc     webrtc.IWebRtc
	Zipper     ZipCompression
}

// State is the commm server state
type State struct {
	reporter                func(state *State)
	services                services
	authMethod              string
	coordinator             *coordinator
	Peers                   []*peer
	topicQueue              chan topicChange
	connectQueue            chan uint64
	webRtcControlQueue      chan *protocol.WebRtcMessage
	messagesQueue           chan *peerMessage
	unregisterQueue         chan *peer
	stop                    chan bool
	softStop                bool
	reportPeriod            time.Duration
	establishSessionTimeout time.Duration

	Alias uint64

	subscriptions     topicSubscriptions
	subscriptionsLock sync.RWMutex
}

func report(state *State) {
	peersCount := len(state.Peers)

	state.subscriptionsLock.RLock()
	state.services.Log.WithFields(logging.Fields{
		"log_type":     "report",
		"peers count":  peersCount,
		"topics count": len(state.subscriptions),
	}).Info("report")
	state.subscriptionsLock.RUnlock()
}

// ICEServer represents a ICEServer config
type ICEServer = webrtc.ICEServer

// Config represents the communication server state
type Config struct {
	AuthMethod              string
	CoordinatorURL          string
	Auth                    authentication.Authentication
	Log                     *logging.Logger
	Marshaller              protocol.IMarshaller
	WebRtc                  webrtc.IWebRtc
	Zipper                  ZipCompression
	EstablishSessionTimeout time.Duration
	ReportPeriod            time.Duration
	Reporter                func(state *State)
	ICEServers              []ICEServer
}

// MakeState creates a new communication server state
func MakeState(config *Config) (*State, error) {
	establishSessionTimeout := config.EstablishSessionTimeout
	if establishSessionTimeout.Seconds() == 0 {
		establishSessionTimeout = 1 * time.Minute
	}

	reportPeriod := config.ReportPeriod
	if reportPeriod.Seconds() == 0 {
		reportPeriod = 30 * time.Second
	}

	if config.Reporter == nil {
		config.Reporter = report
	}

	ss := services{
		Auth:       config.Auth,
		Log:        config.Log,
		Marshaller: config.Marshaller,
		WebRtc:     config.WebRtc,
		Zipper:     config.Zipper,
	}

	if ss.Log == nil {
		ss.Log = logrus.New()
	}

	if ss.Marshaller == nil {
		ss.Marshaller = &protocol.Marshaller{}
	}

	if ss.WebRtc == nil {
		ss.WebRtc = &webrtc.WebRtc{ICEServers: config.ICEServers}
	}

	if ss.Zipper == nil {
		ss.Zipper = &GzipCompression{}
	}

	state := &State{
		services:   ss,
		authMethod: config.AuthMethod,
		coordinator: &coordinator{
			log:  ss.Log,
			url:  config.CoordinatorURL,
			send: make(chan []byte, 256),
		},
		Peers:                   make([]*peer, 0),
		subscriptions:           make(topicSubscriptions),
		stop:                    make(chan bool),
		unregisterQueue:         make(chan *peer, 255),
		topicQueue:              make(chan topicChange, 255),
		connectQueue:            make(chan uint64, 255),
		messagesQueue:           make(chan *peerMessage, 255),
		webRtcControlQueue:      make(chan *protocol.WebRtcMessage, 255),
		reporter:                config.Reporter,
		reportPeriod:            reportPeriod,
		establishSessionTimeout: establishSessionTimeout,
	}

	return state, nil
}

// ConnectCoordinator establish a connection with the coordinator
func ConnectCoordinator(state *State) error {
	c := state.coordinator
	if err := c.Connect(state, state.authMethod); err != nil {
		return err
	}

	welcomeChannel := make(chan *protocol.WelcomeMessage)
	go c.readPump(state, welcomeChannel)
	go c.writePump(state)

	welcomeMessage := <-welcomeChannel

	state.Alias = welcomeMessage.Alias

	connectMessage := protocol.ConnectMessage{Type: protocol.MessageType_CONNECT}

	for _, alias := range welcomeMessage.AvailableServers {
		connectMessage.ToAlias = alias
		c.Send(state, &connectMessage)

		_, err := initPeer(state, alias)
		if err != nil {
			state.services.Log.WithError(err).Error("init peer error creating server (processing welcome)")
			return err
		}
	}

	return nil
}

func closeState(state *State) {
	state.coordinator.Close()
	close(state.webRtcControlQueue)
	close(state.connectQueue)
	close(state.topicQueue)
	close(state.messagesQueue)
	close(state.unregisterQueue)
	close(state.stop)
}

// ProcessMessagesQueue start the TOPIC message processor
func ProcessMessagesQueue(state *State) {
	log := state.services.Log
	for {
		select {
		case msg, ok := <-state.messagesQueue:
			if !ok {
				log.Info("exiting process message loop")
				return
			}
			processTopicMessage(state, msg)
			n := len(state.messagesQueue)
			for i := 0; i < n; i++ {
				msg = <-state.messagesQueue
				processTopicMessage(state, msg)
			}
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

// Process start the peer processor
func Process(state *State) {
	log := state.services.Log

	ticker := time.NewTicker(state.reportPeriod)
	defer ticker.Stop()

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
			processSubscriptionChange(state, change)
			n := len(state.topicQueue)
			for i := 0; i < n; i++ {
				change := <-state.topicQueue
				processSubscriptionChange(state, change)
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
		case <-ticker.C:
			state.reporter(state)
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

func initPeer(state *State, alias uint64) (*peer, error) {
	services := state.services
	log := services.Log
	establishSessionTimeout := state.establishSessionTimeout
	log.WithFields(logging.Fields{"serverAlias": state.Alias, "peer": alias}).Debug("init peer")
	conn, err := services.WebRtc.NewConnection(alias)

	if err != nil {
		log.WithError(err).Error("error creating new peer connection")
		return nil, err
	}

	p := &peer{
		Alias:           alias,
		services:        services,
		Topics:          make(map[string]struct{}),
		Index:           len(state.Peers),
		serverAlias:     state.Alias,
		conn:            conn,
		topicQueue:      state.topicQueue,
		messagesQueue:   state.messagesQueue,
		unregisterQueue: state.unregisterQueue,
	}

	conn.OnICEConnectionStateChange(func(connectionState pion.ICEConnectionState) {
		log.
			WithField("peer", alias).
			WithField("iceConnectionState", connectionState.String()).
			Debugf("ICE Connection State has changed: %s", connectionState.String())
		if connectionState == pion.ICEConnectionStateDisconnected {
			log.WithField("peer", alias).Debug("Connection state is disconnected, closing connection")
			p.Close()
		}
	})

	reliableDC, err := services.WebRtc.CreateReliableDataChannel(conn)
	if err != nil {
		p.logError(err).Error("cannot create new reliable data channel")
		conn.Close()
		return nil, err
	}

	unreliableDC, err := services.WebRtc.CreateUnreliableDataChannel(conn)
	if err != nil {
		p.logError(err).Error("cannot create new unreliable data channel")
		conn.Close()
		return nil, err
	}

	unreliableDCReady := make(chan bool)

	state.Peers = append(state.Peers, p)

	services.WebRtc.RegisterOpenHandler(reliableDC, func() {
		p.log().Info("Reliable data channel open")

		d, err := services.WebRtc.Detach(reliableDC)
		if err != nil {
			p.logError(err).Error("cannot detach data channel")
			p.Close()
			return
		}
		p.ReliableDC = d

		authMessage, err := services.Auth.GenerateAuthMessage(state.authMethod,
			protocol.Role_COMMUNICATION_SERVER)

		if err != nil {
			p.logError(err).Error("cannot create auth message")
			p.Close()
			return
		}

		rawMsg, err := services.Marshaller.Marshal(authMessage)
		if err != nil {
			p.logError(err).Error("cannot encode auth message")
			p.Close()
			return
		}

		if _, err := d.Write(rawMsg); err != nil {
			p.logError(err).Error("error writing message")
			p.Close()
			return
		}

		header := protocol.WorldCommMessage{}
		buffer := make([]byte, maxWorldCommMessageSize)
		n, err := d.Read(buffer)

		if err != nil {
			p.logError(err).Error("datachannel closed before auth")
			p.Close()
			return
		}

		rawMsg = buffer[:n]
		if err := services.Marshaller.Unmarshal(rawMsg, &header); err != nil {
			p.logError(err).Error("decode auth header message failure")
			p.Close()
			return
		}

		msgType := header.GetType()

		if msgType != protocol.MessageType_AUTH {
			p.log().
				WithField("msgType", msgType).
				Info("closing connection: sending data without authorization")
			p.Close()
			return
		}

		if err := services.Marshaller.Unmarshal(rawMsg, authMessage); err != nil {
			p.logError(err).Error("decode auth message failure")
			p.Close()
			return
		}

		if authMessage.Role == protocol.Role_UNKNOWN_ROLE {
			p.logError(err).Error("unknown role")
			p.Close()
			return
		}

		isValid, err := services.Auth.Authenticate(authMessage.Method, authMessage.Role, authMessage.Body)
		if err != nil {
			p.logError(err).Error("authentication error")
			p.Close()
			return
		}

		if isValid {
			p.isServer = authMessage.Role == protocol.Role_COMMUNICATION_SERVER
			p.log().WithField("isServer", p.isServer).Debug("peer authorized")
			if p.isServer {
				state.subscriptionsLock.Lock()
				buffer := bytes.Buffer{}
				i := 0
				last := len(state.subscriptions) - 1
				for topic := range state.subscriptions {
					buffer.WriteString(topic)
					if i != last {
						buffer.WriteString(" ")
					}
					i++
				}
				state.subscriptionsLock.Unlock()

				zipped, err := state.services.Zipper.Zip(buffer.Bytes())
				if err != nil {
					p.logError(err).Error("zip failure")
					p.Close()
					return
				}
				topicSubscriptionMessage := &protocol.TopicSubscriptionMessage{
					Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
					Format: protocol.Format_GZIP,
				}
				topicSubscriptionMessage.Topics = zipped

				rawMsg, err := state.services.Marshaller.Marshal(topicSubscriptionMessage)
				if err != nil {
					p.logError(err).Error("encode topic subscription message failure")
					p.Close()
					return
				}

				if err := p.WriteReliable(rawMsg); err != nil {
					p.logError(err).Error("writing topic subscription message")
					p.Close()
					return
				}
			}
		} else {
			p.log().Info("closing connection: not authorized")
			p.Close()
			return
		}

		go p.readReliablePump()

		<-unreliableDCReady
		go p.readUnreliablePump()
	})

	services.WebRtc.RegisterOpenHandler(unreliableDC, func() {
		p.log().Info("Unreliable data channel open")
		d, err := services.WebRtc.Detach(unreliableDC)
		if err != nil {
			p.logError(err).Error("cannot detach datachannel")
			p.Close()
			return
		}
		p.UnreliableDC = d
		unreliableDCReady <- true
	})

	go func() {
		time.Sleep(establishSessionTimeout)
		if services.WebRtc.IsNew(conn) {
			p.log().
				Info("ICEConnectionStateNew after establish timeout, closing connection and queued to unregister")
			p.Close()
		}
	}()

	return p, nil
}

func processUnregister(state *State, p *peer) {
	if p.Index == -1 {
		return
	}

	log := state.services.Log

	alias := p.Alias
	log.WithField("peer", alias).Debug("unregister peer")

	if state.Peers[p.Index] != p {
		panic("inconsistency detected in peer tracking")
	}

	size := len(state.Peers)
	last := state.Peers[size-1]
	state.Peers[p.Index] = last
	last.Index = p.Index
	state.Peers = state.Peers[:size-1]

	if p.isServer {
		state.subscriptionsLock.Lock()
		for topic := range p.Topics {
			state.subscriptions.RemoveServerSubscription(topic, p)
		}
		state.subscriptionsLock.Unlock()
	} else {
		topicsChanged := false

		state.subscriptionsLock.Lock()
		for topic := range p.Topics {
			if state.subscriptions.RemoveClientSubscription(topic, p) {
				topicsChanged = true
			}
		}
		state.subscriptionsLock.Unlock()

		if topicsChanged {
			broadcastSubscriptionChange(state)
		}
	}

	p.Index = -1
}

func processConnect(state *State, alias uint64) error {
	log := state.services.Log

	oldP := findPeer(state.Peers, alias)
	if oldP != nil && !oldP.IsClosed() {
		processUnregister(state, oldP)
	}

	p, err := initPeer(state, alias)
	if err != nil {
		return err
	}

	offer, err := state.services.WebRtc.CreateOffer(p.conn)
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

func processSubscriptionChange(state *State, change topicChange) error {
	log := state.services.Log
	p := change.peer

	topicsChanged := false

	rawTopics := change.rawTopics
	if change.format == protocol.Format_GZIP {
		unzipedTopics, err := state.services.Zipper.Unzip(rawTopics)

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

			state.subscriptionsLock.Lock()
			if p.isServer {
				state.subscriptions.AddServerSubscription(topic, p)
			} else {
				if state.subscriptions.AddClientSubscription(topic, p) {
					topicsChanged = true
				}
			}
			state.subscriptionsLock.Unlock()
		}
	}

	sort.Strings(newTopics)

	// NOTE: check if topics were deleted
	for topic := range p.Topics {
		if sort.SearchStrings(newTopics, topic) < len(newTopics) {
			continue
		}

		delete(p.Topics, topic)

		state.subscriptionsLock.Lock()
		if p.isServer {
			state.subscriptions.RemoveServerSubscription(topic, p)
		} else {
			if state.subscriptions.RemoveClientSubscription(topic, p) {
				topicsChanged = true
			}
		}
		state.subscriptionsLock.Unlock()
	}

	if topicsChanged {
		return broadcastSubscriptionChange(state)
	}

	return nil
}

func processWebRtcControlMessage(state *State, webRtcMessage *protocol.WebRtcMessage) error {
	log := state.services.Log

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
		p.log().Debug("webrtc offer received")
		answer, err := state.services.WebRtc.OnOffer(p.conn, webRtcMessage.Sdp)
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
		p.log().Debug("webrtc answer received")
		if err := state.services.WebRtc.OnAnswer(p.conn, webRtcMessage.Sdp); err != nil {
			log.WithError(err).Error("error setting webrtc answer")
			return err
		}
	case protocol.MessageType_WEBRTC_ICE_CANDIDATE:
		p.log().Debug("ice candidate received")
		if err := state.services.WebRtc.OnIceCandidate(p.conn, webRtcMessage.Sdp); err != nil {
			log.WithError(err).Error("error adding remote ice candidate")
			return err
		}
	default:
		log.Fatal(errors.New("invalid message type in processWebRtcControlMessage"))
	}

	return nil
}

func processTopicMessage(state *State, msg *peerMessage) {
	topic := msg.topic
	fromServer := msg.fromServer
	reliable := msg.reliable

	state.subscriptionsLock.RLock()
	defer state.subscriptionsLock.RUnlock()
	subscription, ok := state.subscriptions[topic]

	if !ok {
		return
	}

	for _, p := range subscription.clients {
		if p == msg.from || p.IsClosed() {
			continue
		}

		rawMsg := msg.rawMsgToClient
		if reliable {
			if err := p.WriteReliable(rawMsg); err != nil {
				p.logError(err).Error("error writing reliable message to client")
				continue
			}
		} else {
			if err := p.WriteUnreliable(rawMsg); err != nil {
				p.logError(err).Error("error writing unreliable message to client")
				continue
			}
		}
	}

	if !fromServer && msg.rawMsgToServer != nil {
		for _, p := range subscription.servers {
			if p.IsClosed() {
				continue
			}
			rawMsg := msg.rawMsgToServer
			if reliable {
				if err := p.WriteReliable(rawMsg); err != nil {
					p.logError(err).Error("error writing reliable message to server")
					continue
				}
			} else {
				if err := p.WriteUnreliable(rawMsg); err != nil {
					p.logError(err).Error("error writing unreliable message to server")
					continue
				}
			}
		}
	}
}

func broadcastSubscriptionChange(state *State) error {
	log := state.services.Log

	state.subscriptionsLock.RLock()
	buffer := bytes.Buffer{}
	i := 0
	last := len(state.subscriptions) - 1
	for topic := range state.subscriptions {
		buffer.WriteString(topic)
		if i != last {
			buffer.WriteString(" ")
		}
		i++
	}
	state.subscriptionsLock.RUnlock()

	encodedTopics, err := state.services.Zipper.Zip(buffer.Bytes())
	if err != nil {
		return err
	}

	message := &protocol.TopicSubscriptionMessage{
		Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
		Format: protocol.Format_GZIP,
		Topics: encodedTopics,
	}

	rawMsg, err := state.services.Marshaller.Marshal(message)
	if err != nil {
		log.WithError(err).Error("encode topic subscription message failure")
		return err
	}

	for _, p := range state.Peers {
		if p.isServer && p.ReliableDC != nil {
			p.log().Debug("send topic change (to server)")
			if err := p.WriteReliable(rawMsg); err != nil {
				continue
			}
		}
	}

	return nil
}
