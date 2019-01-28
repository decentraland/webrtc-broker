package worldcomm

import (
	"errors"
	"time"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/webrtc"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
)

const (
	reportPeriod            = 30 * time.Second
	maxWorldCommMessageSize = 1024
	logTopicMessageReceived = false
	logAddTopic             = false
	logRemoveTopic          = false
)

type TopicOp int

const (
	AddTopic TopicOp = iota + 1
	RemoveTopic
)

type topicChange struct {
	op    TopicOp
	alias string
	topic string
}

type peerMessage struct {
	fromServer bool
	reliable   bool
	topic      string
	from       string
	bytes      []byte
}

type peer struct {
	isServer           bool
	IsAuthenticated    bool
	authenticationSent bool
	isClosed           bool
	alias              string
	sendReliable       chan []byte
	sendUnreliable     chan []byte
	conn               webrtc.IWebRtcConnection
	Topics             map[string]bool
}

type PeersIndex = map[string]map[string]bool

type WorldCommunicationState struct {
	Alias              string
	authMethod         string
	Auth               authentication.Authentication
	marshaller         protocol.IMarshaller
	log                *logging.Logger
	webRtc             webrtc.IWebRtc
	coordinator        *Coordinator
	Peers              map[string]*peer
	peersIndex         PeersIndex
	subscriptions      map[string]bool
	topicQueue         chan *topicChange
	connectQueue       chan string
	webRtcControlQueue chan *protocol.WebRtcMessage
	messagesQueue      chan *peerMessage
	unregisterQueue    chan string
	aliasChannel       chan string
	stop               chan bool
	softStop           bool
	agent              worldCommAgent
}

func MakeState(agent agent.IAgent, authMethod string, coordinatorUrl string) WorldCommunicationState {
	return WorldCommunicationState{
		authMethod:         authMethod,
		Auth:               authentication.Make(),
		marshaller:         &protocol.Marshaller{},
		log:                logging.New(),
		webRtc:             &webrtc.WebRtc{},
		agent:              worldCommAgent{agent: agent},
		coordinator:        makeCoordinator(coordinatorUrl),
		Peers:              make(map[string]*peer),
		peersIndex:         make(PeersIndex),
		subscriptions:      make(map[string]bool),
		stop:               make(chan bool),
		unregisterQueue:    make(chan string, 255),
		aliasChannel:       make(chan string),
		topicQueue:         make(chan *topicChange, 255),
		connectQueue:       make(chan string, 255),
		messagesQueue:      make(chan *peerMessage, 255),
		webRtcControlQueue: make(chan *protocol.WebRtcMessage, 255),
	}
}

func (p *peer) Close() {
	if !p.isClosed {
		if p.conn != nil {
			p.conn.Close()
		}
		p.isClosed = true
	}
}

func readPeerMessage(state *WorldCommunicationState, p *peer, reliable bool, bytes []byte, topicMessage *protocol.TopicMessage) {
	log := state.log
	marshaller := state.marshaller

	if err := marshaller.Unmarshal(bytes, topicMessage); err != nil {
		log.WithError(err).Debug("decode topic message failure")
		return
	}

	if logTopicMessageReceived {
		log.WithFields(logging.Fields{
			"peer":     p.alias,
			"reliable": reliable,
			"topic":    topicMessage.Topic,
		}).Debug("message received")
	}

	if p.isServer {
		msg := &peerMessage{
			fromServer: true,
			reliable:   reliable,
			topic:      topicMessage.Topic,
			from:       p.alias,
			bytes:      bytes,
		}

		state.messagesQueue <- msg
	} else {
		topicMessage.FromAlias = p.alias
		bytes, err := marshaller.Marshal(topicMessage)
		if err != nil {
			log.WithError(err).Error("encode topic message failure")
			return
		}

		msg := &peerMessage{
			fromServer: false,
			reliable:   reliable,
			topic:      topicMessage.Topic,
			from:       p.alias,
			bytes:      bytes,
		}

		state.messagesQueue <- msg
	}
}

func (p *peer) readReliablePump(state *WorldCommunicationState) {
	marshaller := state.marshaller
	auth := state.Auth
	log := state.log
	header := &protocol.WorldCommMessage{}
	changeTopicMessage := &protocol.ChangeTopicMessage{}
	topicMessage := &protocol.TopicMessage{}

	buffer := make([]byte, maxWorldCommMessageSize)

	for {
		n, err := p.conn.ReadReliable(buffer)

		if err != nil {
			log.WithError(err).WithFields(logging.Fields{
				"peer": p.alias,
				"id":   state.Alias,
			}).Info("exit peer.readReliablePump(), datachannel closed")

			p.Close()
			state.unregisterQueue <- p.alias
			return
		}

		if n == 0 {
			continue
		}

		state.agent.RecordReceivedReliableFromPeerSize(n)
		bytes := buffer[:n]
		if err := marshaller.Unmarshal(bytes, header); err != nil {
			log.WithField("peer", p.alias).WithError(err).Debug("decode header message failure")
			continue
		}

		msgType := header.GetType()

		if !p.IsAuthenticated && msgType != protocol.MessageType_AUTH {
			log.WithField("peer", p.alias).Debug("closing connection: sending data without authorization")
			p.Close()
			break
		}

		switch msgType {
		case protocol.MessageType_AUTH:
			authMessage := &protocol.AuthMessage{}
			if err := marshaller.Unmarshal(bytes, authMessage); err != nil {
				log.WithField("peer", p.alias).WithError(err).Debug("decode auth message failure")
				continue
			}

			if authMessage.Role == protocol.Role_UNKNOWN_ROLE {
				log.WithField("peer", p.alias).WithError(err).Debug("unknown role")
				continue
			}

			isValid, err := auth.Authenticate(authMessage.Method, authMessage.Role, authMessage.Body)
			if err != nil {
				log.WithField("peer", p.alias).WithError(err).Error("authentication error")
				p.Close()
				break
			}

			if isValid {
				p.isServer = authMessage.Role == protocol.Role_COMMUNICATION_SERVER
				p.IsAuthenticated = true

				log.WithFields(logging.Fields{
					"id":       state.Alias,
					"peer":     p.alias,
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

						bytes, err := state.marshaller.Marshal(authMessage)
						if err != nil {
							log.WithError(err).Error("cannot encode auth message")
							return
						}

						p.sendReliable <- bytes
					}

					for topic := range state.subscriptions {
						changeTopicMessage.Type = protocol.MessageType_ADD_TOPIC
						changeTopicMessage.Topic = topic

						bytes, err := state.marshaller.Marshal(changeTopicMessage)
						if err != nil {
							log.WithError(err).Error("encode add topic message failure")
							p.Close()
							break
						}

						p.sendReliable <- bytes
					}
				}
			} else {
				log.WithField("peer", p.alias).Debug("closing connection: not authorized")
				p.Close()
				break
			}
		case protocol.MessageType_ADD_TOPIC:
			if err := marshaller.Unmarshal(bytes, changeTopicMessage); err != nil {
				log.WithField("peer", p.alias).WithError(err).Debug("decode add topic message failure")
				continue
			}

			topic := changeTopicMessage.Topic

			if logAddTopic {
				log.WithFields(logging.Fields{
					"id":    state.Alias,
					"peer":  p.alias,
					"topic": topic,
				}).Debug("add topic message")
			}

			state.topicQueue <- &topicChange{op: AddTopic, alias: p.alias, topic: topic}
		case protocol.MessageType_REMOVE_TOPIC:
			if err := marshaller.Unmarshal(bytes, changeTopicMessage); err != nil {
				log.WithError(err).Debug("decode remove topic message failure")
				continue
			}
			topic := changeTopicMessage.Topic
			if logRemoveTopic {
				log.WithFields(logging.Fields{
					"id":    state.Alias,
					"peer":  p.alias,
					"topic": topic,
				}).Debug("remove topic message")
			}
			state.topicQueue <- &topicChange{op: RemoveTopic, alias: p.alias, topic: topic}
		case protocol.MessageType_TOPIC:
			readPeerMessage(state, p, true, bytes, topicMessage)
		case protocol.MessageType_PING:
			p.sendUnreliable <- bytes
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
	for {
		n, err := p.conn.ReadUnreliable(buffer)

		if err != nil {
			log.WithError(err).WithFields(logging.Fields{
				"peer": p.alias,
				"id":   state.Alias,
			}).Info("exit peer.readUnreliablePump(), datachannel closed")

			p.Close()
			state.unregisterQueue <- p.alias
			return
		}

		if n == 0 {
			continue
		}

		state.agent.RecordReceivedUnreliableFromPeerSize(n)
		bytes := buffer[:n]
		if err := marshaller.Unmarshal(bytes, header); err != nil {
			log.WithField("peer", p.alias).WithError(err).Debug("decode header message failure")
			continue
		}

		msgType := header.GetType()

		if !p.IsAuthenticated && msgType != protocol.MessageType_AUTH {
			log.WithField("peer", p.alias).Debug("closing connection: sending data without authorization")
			p.Close()
			return
		}

		switch msgType {
		case protocol.MessageType_TOPIC:
			readPeerMessage(state, p, false, bytes, topicMessage)
		case protocol.MessageType_PING:
			p.sendUnreliable <- bytes
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
		case bytes, ok := <-p.sendReliable:
			if !ok {
				log.WithFields(logging.Fields{
					"peer": p.alias,
					"id":   state.Alias,
				}).Info("exit peer.writePump(), sendReliable channel closed")
				return
			}

			if err := p.conn.WriteReliable(bytes); err != nil {
				log.WithField("peer", p.alias).WithError(err).Error("error writing message")
				return
			}

			state.agent.RecordSentReliableToPeerSize(len(bytes))
			n := len(p.sendReliable)
			for i := 0; i < n; i++ {
				bytes = <-p.sendReliable
				if err := p.conn.WriteReliable(bytes); err != nil {
					log.WithField("peer", p.alias).WithError(err).Error("error writing message")
					return
				}
				state.agent.RecordSentReliableToPeerSize(len(bytes))
			}
		case bytes, ok := <-p.sendUnreliable:
			if !ok {
				log.WithFields(logging.Fields{
					"peer": p.alias,
					"id":   state.Alias,
				}).Info("exit peer.writePump(), sendUnreliable channel closed")
				return
			}

			if err := p.conn.WriteUnreliable(bytes); err != nil {
				log.WithField("peer", p.alias).WithError(err).Error("error writing message")
				return
			}

			state.agent.RecordSentUnreliableToPeerSize(len(bytes))
			n := len(p.sendUnreliable)
			for i := 0; i < n; i++ {
				bytes = <-p.sendUnreliable
				if err := p.conn.WriteUnreliable(bytes); err != nil {
					log.WithField("peer", p.alias).WithError(err).Error("error writing message")
					return
				}
				state.agent.RecordSentUnreliableToPeerSize(len(bytes))
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

	state.Alias = <-state.aliasChannel

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
	ticker := time.NewTicker(reportPeriod)
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
			processTopicChange(state, change)
			n := len(state.topicQueue)
			for i := 0; i < n; i++ {
				change := <-state.topicQueue
				processTopicChange(state, change)
			}
		case alias := <-state.unregisterQueue:
			processUnregister(state, alias)
			n := len(state.unregisterQueue)
			for i := 0; i < n; i++ {
				alias = <-state.unregisterQueue
				processUnregister(state, alias)
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
		case <-ticker.C:
			state.agent.RecordTotalPeerConnections(len(state.Peers))
			state.agent.RecordTotalTopicSubscriptions(len(state.Peers))

			log.WithFields(logging.Fields{
				"peers count":  len(state.Peers),
				"topics count": len(state.subscriptions),
			}).Debug("report")

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
		"peer": alias,
		"id":   state.Alias,
	}).Debug("init peer")
	conn, err := state.webRtc.NewConnection()

	if err != nil {
		log.WithError(err).Error("error creating new peer connection")
		return nil, err
	}

	p := &peer{
		sendReliable:   make(chan []byte, 256),
		sendUnreliable: make(chan []byte, 256),
		alias:          alias,
		conn:           conn,
		Topics:         make(map[string]bool),
	}
	state.Peers[alias] = p

	conn.OnReliableChannelOpen(func() {
		go p.writePump(state)
		go p.readReliablePump(state)
	})

	conn.OnUnreliableChannelOpen(func() {
		go p.readUnreliablePump(state)
	})

	return p, nil
}

func processUnregister(state *WorldCommunicationState, alias string) {
	log := state.log
	p := state.Peers[alias]
	if p != nil {
		log.WithField("peer", alias).Debug("unregister peer")
		delete(state.Peers, alias)

		close(p.sendReliable)
		close(p.sendUnreliable)

		for topic := range p.Topics {
			peers := state.peersIndex[topic]
			if peers != nil {
				delete(peers, alias)
			}

			// NOTE if a client is unsubscribe for a topic,
			// check if there is at least one other client subscription,
			// if not, cancel it.
			if !p.isServer && state.subscriptions[topic] {
				shouldRemoveTopic := true

				for alias := range peers {
					p := state.Peers[alias]

					if !p.isServer {
						shouldRemoveTopic = false
						break
					}
				}

				if shouldRemoveTopic {
					broadcastTopicChange(state, protocol.MessageType_REMOVE_TOPIC, topic)
					delete(state.subscriptions, topic)
				}
			}
		}
	}
}

func processConnect(state *WorldCommunicationState, alias string) error {
	log := state.log

	processUnregister(state, alias)

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

func processTopicChange(state *WorldCommunicationState, change *topicChange) {
	alias := change.alias
	topic := change.topic

	p := state.Peers[alias]

	if p == nil {
		return
	}

	peers := state.peersIndex[topic]

	if peers == nil {
		peers = make(map[string]bool)
		state.peersIndex[topic] = peers
	}

	if change.op == AddTopic {
		peers[alias] = true
		p.Topics[topic] = true
		if !p.isServer && !state.subscriptions[topic] {
			broadcastTopicChange(state, protocol.MessageType_ADD_TOPIC, topic)
			state.subscriptions[topic] = true
		}
	} else {
		delete(peers, alias)
		delete(p.Topics, topic)

		// NOTE if a client is unsubscribe for a topic, check if there is at least one other client subscription,
		// if not, cancel it.
		if !p.isServer && state.subscriptions[topic] {
			shouldRemoveTopic := true

			for alias := range peers {
				p := state.Peers[alias]

				if !p.isServer {
					shouldRemoveTopic = false
					break
				}
			}

			if shouldRemoveTopic {
				broadcastTopicChange(state, protocol.MessageType_REMOVE_TOPIC, topic)
				delete(state.subscriptions, topic)
			}
		}
	}
}

func processWebRtcControlMessage(state *WorldCommunicationState, webRtcMessage *protocol.WebRtcMessage) error {
	log := state.log

	alias := webRtcMessage.FromAlias
	p := state.Peers[alias]
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
			"id":   state.Alias,
			"peer": p.alias,
		}).Debug("webrtc offer received")
		answer, err := p.conn.OnOffer(webRtcMessage.Sdp)
		if err != nil {
			log.WithError(err).Error("error setting webrtc offer")
			return err
		}

		state.coordinator.Send(state, &protocol.WebRtcMessage{
			Type:    protocol.MessageType_WEBRTC_ANSWER,
			Sdp:     answer,
			ToAlias: p.alias,
		})
	case protocol.MessageType_WEBRTC_ANSWER:
		log.WithFields(logging.Fields{
			"id":   state.Alias,
			"peer": p.alias,
		}).Debug("webrtc answer received")
		if err := p.conn.OnAnswer(webRtcMessage.Sdp); err != nil {
			log.WithError(err).Error("error setting webrtc answer")
			return err
		}
	case protocol.MessageType_WEBRTC_ICE_CANDIDATE:
		log.WithFields(logging.Fields{
			"id":   state.Alias,
			"peer": p.alias,
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
	peers := state.peersIndex[msg.topic]
	if peers == nil {
		return
	}

	for alias := range peers {
		if alias != msg.from {
			p := state.Peers[alias]

			if p == nil || p.isClosed {
				continue
			}

			if msg.fromServer && p.isServer {
				continue
			}

			if msg.reliable {
				p.sendReliable <- msg.bytes
			} else {
				p.sendUnreliable <- msg.bytes
			}
		}
	}
}

func broadcastTopicChange(state *WorldCommunicationState, msgType protocol.MessageType, topic string) error {
	log := state.log
	changeTopicMessage := &protocol.ChangeTopicMessage{
		Type:  msgType,
		Topic: topic,
	}

	bytes, err := state.marshaller.Marshal(changeTopicMessage)
	if err != nil {
		log.WithError(err).Error("encode change topic message failure")
		return err
	}

	for _, p := range state.Peers {
		if p.isServer {
			log.WithFields(logging.Fields{
				"id":   state.Alias,
				"peer": p.alias,
			}).Debug("send topic change (to server)")
			p.sendReliable <- bytes
		}
	}

	return nil
}
