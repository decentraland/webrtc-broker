// Package server implements a basic webrtc server layer
package server

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/decentraland/webrtc-broker/internal/logging"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"

	pion "github.com/pion/webrtc/v2"
)

// Config represents the server config
type Config struct {
	Log                     *logging.Logger
	WebRtc                  IWebRtc
	EstablishSessionTimeout time.Duration
	ICEServers              []ICEServer
	ExitOnCoordinatorClose  bool
	MaxPeers                uint16
	WebRtcLogLevel          zerolog.Level

	OnNewPeerHdlr          func(p *Peer) error
	OnPeerDisconnectedHdlr func(p *Peer)
}

// Server ...
type Server struct {
	Alias                   uint64
	webRtc                  IWebRtc
	log                     logging.Logger
	coordinator             *coordinator
	peers                   []*Peer
	peersMux                sync.Mutex
	connectCh               chan uint64
	webRtcControlCh         chan *protocol.WebRtcMessage
	unregisterCh            chan *Peer
	establishSessionTimeout time.Duration
	maxPeers                uint16

	onNewPeerHdlr          func(p *Peer) error
	onPeerDisconnectedHdlr func(p *Peer)
}

// Peer represents a server connection
type Peer struct {
	webRtc IWebRtc

	Alias uint64
	index int

	unregisterCh chan *Peer

	Conn *PeerConnection

	candidatesMux     sync.Mutex
	pendingCandidates []*ICECandidate

	Log logging.Logger
}

// IsClosed is the underline webrtc connection is closed
func (p *Peer) IsClosed() bool {
	return p.webRtc.isClosed(p.Conn)
}

// Close ...
func (p *Peer) Close() {
	if err := p.webRtc.close(p.Conn); err != nil {
		p.Log.Warn().Err(err).Msg("error closing connection")
		return
	}

	p.unregisterCh <- p
}

func findPeer(peers []*Peer, alias uint64) *Peer {
	for _, p := range peers {
		if p.Alias == alias {
			return p
		}
	}

	return nil
}

// NewServer creates a new communication server
func NewServer(config *Config) (*Server, error) {
	establishSessionTimeout := config.EstablishSessionTimeout
	if establishSessionTimeout.Seconds() == 0 {
		establishSessionTimeout = 1 * time.Minute
	}

	var log logging.Logger
	if config.Log == nil {
		log = logging.New()
	} else {
		log = *config.Log
	}

	server := &Server{
		coordinator: &coordinator{
			log:         log,
			send:        make(chan []byte, 256),
			exitOnClose: config.ExitOnCoordinatorClose,
		},
		peers:                   make([]*Peer, 0),
		unregisterCh:            make(chan *Peer, 255),
		connectCh:               make(chan uint64, 255),
		webRtcControlCh:         make(chan *protocol.WebRtcMessage, 255),
		establishSessionTimeout: establishSessionTimeout,
		webRtc:                  config.WebRtc,
		onNewPeerHdlr:           config.OnNewPeerHdlr,
		onPeerDisconnectedHdlr:  config.OnPeerDisconnectedHdlr,
		maxPeers:                config.MaxPeers,
	}

	server.log = log

	if server.webRtc == nil {
		server.webRtc = &webRTC{ICEServers: config.ICEServers, LogLevel: config.WebRtcLogLevel}
	}

	return server, nil
}

// ConnectCoordinator establish a connection with the coordinator
func (s *Server) ConnectCoordinator(url string) (*protocol.WelcomeMessage, error) {
	c := s.coordinator
	if err := c.Connect(s, url); err != nil {
		return nil, err
	}

	welcomeChannel := make(chan *protocol.WelcomeMessage)

	go c.readPump(s, welcomeChannel)

	go c.writePump()

	welcomeMessage := <-welcomeChannel

	s.Alias = welcomeMessage.Alias

	return welcomeMessage, nil
}

// ConnectPeer initiates the peer connection, it will send a connect message thought the coordinator
// and create the peer itself
func (s *Server) ConnectPeer(alias uint64) error {
	connectMessage := protocol.ConnectMessage{
		Type:    protocol.MessageType_CONNECT,
		ToAlias: alias,
	}

	if err := s.coordinator.Send(&connectMessage); err != nil {
		return err
	}

	if _, err := s.initPeer(alias); err != nil {
		return err
	}

	return nil
}

// ProcessControlMessages starts the control message processor
func (s *Server) ProcessControlMessages() {
	defer func() {
		for _, p := range s.peers {
			p.Close()
		}
	}()

	ignoreError := func(err error) {
		if err != nil {
			s.log.Debug().Err(err).Msg("ignoring error")
		}
	}

	for {
		select {
		case alias, ok := <-s.connectCh:
			if !ok {
				s.log.Info().Str("channel", "connect").Msg("channel close, exiting control loop")
				return
			}

			ignoreError(s.processConnect(alias))

			n := len(s.connectCh)
			for i := 0; i < n; i++ {
				alias, ok := <-s.connectCh
				if !ok {
					s.log.Info().Str("channel", "connect").Msg("channel close, exiting control loop")
					return
				}

				ignoreError(s.processConnect(alias))
			}
		case p, ok := <-s.unregisterCh:
			if !ok {
				s.log.Info().Str("channel", "unregister").Msg("channel close, exiting control loop")
				return
			}

			ignoreError(s.processUnregister(p))

			n := len(s.unregisterCh)
			for i := 0; i < n; i++ {
				p, ok = <-s.unregisterCh
				if !ok {
					s.log.Info().Str("channel", "unregister").Msg("channel close, exiting control loop")
					return
				}

				ignoreError(s.processUnregister(p))
			}
		case webRtcMessage, ok := <-s.webRtcControlCh:
			if !ok {
				s.log.Info().Str("channel", "webrtc").Msg("channel close, exiting control loop")
				return
			}

			ignoreError(s.processWebRtcControlMessage(webRtcMessage))

			n := len(s.webRtcControlCh)
			for i := 0; i < n; i++ {
				webRtcMessage, ok = <-s.webRtcControlCh
				if !ok {
					s.log.Info().Str("channel", "webrtc").Msg("channel close, exiting control loop")
					return
				}

				ignoreError(s.processWebRtcControlMessage(webRtcMessage))
			}
		}
	}
}

// Shutdown ...
// NOTE(hugo): we cannot close the unregisterCh because it's
// shared with peers, we would need to wait for peers to be unnregistered first
func Shutdown(server *Server) {
	server.coordinator.Close()
	close(server.webRtcControlCh)
	close(server.connectCh)
}

func (s *Server) sendICECandidate(alias uint64, candidate *ICECandidate) {
	iceCandidateInit := candidate.ToJSON()

	serializedCandidate, err := json.Marshal(iceCandidateInit)
	if err != nil {
		s.log.Error().Uint64("peer", alias).Err(err).Msg("cannot serialize candidate")
		return
	}

	err = s.coordinator.Send(&protocol.WebRtcMessage{
		Type:    protocol.MessageType_WEBRTC_ICE_CANDIDATE,
		Data:    serializedCandidate,
		ToAlias: alias,
	})
	if err != nil {
		s.log.Error().Uint64("peer", alias).Err(err).Msg("cannot send ICE candidate")
		return
	}
}

func (s *Server) initPeer(alias uint64) (*Peer, error) {
	if s.maxPeers > 0 {
		s.peersMux.Lock()
		size := len(s.peers)
		s.peersMux.Unlock()

		if uint16(size+1) > s.maxPeers {
			s.log.Info().Uint64("peer", alias).Msg("reject peer, the server is full")

			refusedMessage := protocol.ConnectionRefusedMessage{
				Type:    protocol.MessageType_CONNECTION_REFUSED,
				ToAlias: alias,
				Reason:  protocol.ConnectionRefusedReason_SERVER_FULL,
			}

			if err := s.coordinator.Send(&refusedMessage); err != nil {
				s.log.Info().Uint64("peer", alias).Msg("cannot send refused connection message")
			}

			return nil, errors.New("server full")
		}
	}

	establishSessionTimeout := s.establishSessionTimeout
	s.log.Debug().Uint64("serverAlias", s.Alias).Uint64("peer", alias).Msg("init peer")

	conn, err := s.webRtc.newConnection(alias)
	if err != nil {
		s.log.Error().Err(err).Msg("error creating new peer connection")
		return nil, err
	}

	p := &Peer{
		webRtc:       s.webRtc,
		Alias:        alias,
		index:        len(s.peers),
		Conn:         conn,
		unregisterCh: s.unregisterCh,
		Log:          s.log.With().Uint64("serverAlias", s.Alias).Uint64("peer", alias).Logger(),
	}

	conn.OnICECandidate(func(candidate *ICECandidate) {
		if candidate == nil {
			s.log.Debug().Uint64("peer", alias).Msg("finish collecting candidates")
			return
		}

		p.candidatesMux.Lock()
		defer p.candidatesMux.Unlock()

		desc := conn.RemoteDescription()
		if desc == nil {
			p.pendingCandidates = append(p.pendingCandidates, candidate)
		} else {
			s.sendICECandidate(alias, candidate)
		}
	})

	conn.OnICEConnectionStateChange(func(connectionState pion.ICEConnectionState) {
		s.log.Debug().
			Uint64("peer", alias).
			Str("iceConnectionState", connectionState.String()).
			Msg("ICE Connection State has changed")
		if connectionState == pion.ICEConnectionStateDisconnected {
			s.log.Debug().Uint64("peer", alias).Msg("Connection state is disconnected, closing connection")
			p.Close()
		} else if connectionState == pion.ICEConnectionStateFailed {
			s.log.Debug().Uint64("peer", alias).Msg("Connection state is failed, closing connection")
			p.Close()
		}
	})

	if s.onNewPeerHdlr != nil {
		err = s.onNewPeerHdlr(p)
		if err != nil {
			return nil, err
		}
	}

	s.peersMux.Lock()
	s.peers = append(s.peers, p)
	s.peersMux.Unlock()

	go func() {
		time.Sleep(establishSessionTimeout)

		if s.webRtc.isNew(conn) {
			p.Log.Info().
				Msg("ICEConnectionStateNew after establish timeout, closing connection and queued to unregister")
			p.Close()
		}
	}()

	return p, nil
}

func (s *Server) processUnregister(p *Peer) error {
	if p.index == -1 {
		return nil
	}

	alias := p.Alias
	s.log.Debug().Uint64("peer", alias).Msg("unregister peer")

	s.peersMux.Lock()
	pp := s.peers[p.index]
	s.peersMux.Unlock()

	if p != pp {
		s.log.Error().Uint64("peer", alias).Msg("inconsistency detected in peer tracking")
		panic("inconsistency detected in peer tracking")
	}

	s.peersMux.Lock()
	size := len(s.peers)
	last := s.peers[size-1]
	s.peers[p.index] = last
	last.index = p.index
	s.peers = s.peers[:size-1]
	p.index = -1
	s.peersMux.Unlock()

	if s.onPeerDisconnectedHdlr != nil {
		s.onPeerDisconnectedHdlr(p)
	}

	return nil
}

func (s *Server) processConnect(alias uint64) error {
	oldP := findPeer(s.peers, alias)
	if oldP != nil && !oldP.IsClosed() {
		if err := s.processUnregister(oldP); err != nil {
			return err
		}
	}

	p, err := s.initPeer(alias)
	if err != nil {
		return err
	}

	offer, err := s.webRtc.createOffer(p.Conn)
	if err != nil {
		s.log.Error().Err(err).Uint64("peer", alias).Msg("cannot create offer")
		return err
	}

	serializedOffer, err := json.Marshal(offer)
	if err != nil {
		s.log.Error().Err(err).Uint64("peer", alias).Msg("cannot serialize offer")
		return err
	}

	return s.coordinator.Send(&protocol.WebRtcMessage{
		Type:    protocol.MessageType_WEBRTC_OFFER,
		Data:    serializedOffer,
		ToAlias: alias,
	})
}

func (s *Server) processWebRtcControlMessage(webRtcMessage *protocol.WebRtcMessage) error {
	alias := webRtcMessage.FromAlias

	p := findPeer(s.peers, alias)
	if p == nil {
		var err error

		p, err = s.initPeer(alias)
		if err != nil {
			return err
		}
	}

	switch webRtcMessage.Type {
	case protocol.MessageType_WEBRTC_OFFER:
		p.Log.Debug().Msg("webrtc offer received")

		offer := pion.SessionDescription{}

		if err := json.Unmarshal(webRtcMessage.Data, &offer); err != nil {
			p.Log.Error().Err(err).Msg("error unmarshalling offer")
			return err
		}

		answer, err := s.webRtc.onOffer(p.Conn, offer)
		if err != nil {
			p.Log.Error().Err(err).Msg("error setting webrtc offer")
			return err
		}

		serializedAnswer, err := json.Marshal(answer)
		if err != nil {
			p.Log.Error().Err(err).Msg("cannot serialize answer")
			return err
		}

		return s.coordinator.Send(&protocol.WebRtcMessage{
			Type:    protocol.MessageType_WEBRTC_ANSWER,
			Data:    serializedAnswer,
			ToAlias: p.Alias,
		})
	case protocol.MessageType_WEBRTC_ANSWER:
		p.Log.Debug().Msg("webrtc answer received")

		answer := pion.SessionDescription{}
		if err := json.Unmarshal(webRtcMessage.Data, &answer); err != nil {
			p.Log.Error().Err(err).Msg("error unmarshalling answer")
			return err
		}

		p.candidatesMux.Lock()
		if err := s.webRtc.onAnswer(p.Conn, answer); err != nil {
			p.Log.Error().Err(err).Msg("error settinng webrtc answer")
			return err
		}

		for _, c := range p.pendingCandidates {
			s.sendICECandidate(p.Alias, c)
		}
		p.candidatesMux.Unlock()
	case protocol.MessageType_WEBRTC_ICE_CANDIDATE:
		p.Log.Debug().Msg("ice candidate received")

		candidate := pion.ICECandidateInit{}
		if err := json.Unmarshal(webRtcMessage.Data, &candidate); err != nil {
			p.Log.Error().Err(err).Msg("error unmarshalling candidate")
			return err
		}

		if err := s.webRtc.onIceCandidate(p.Conn, candidate); err != nil {
			p.Log.Error().Err(err).Msg("error adding remote ice candidate")
			return err
		}
	default:
		s.log.Fatal().Msg("invalid message type in processWebRtcControlMessage")
	}

	return nil
}

// Stats ...
type Stats struct {
	Time                time.Time
	Alias               uint64
	ConnectChSize       int
	WebRtcControlChSize int
	UnregisterChSize    int
	Peers               []PeerStats
}

// PeerStats ...
type PeerStats struct {
	pion.StatsReport
	Alias uint64
}

// Get get stats by stats ID
func (ps *PeerStats) Get(id string) (pion.Stats, bool) {
	stats, ok := ps.StatsReport[id]
	return stats, ok
}

// GetServerStats ...
func (s *Server) GetServerStats() Stats {
	s.peersMux.Lock()
	defer s.peersMux.Unlock()

	serverStats := Stats{
		Time:                time.Now(),
		Alias:               s.Alias,
		Peers:               make([]PeerStats, len(s.peers)),
		ConnectChSize:       len(s.connectCh),
		WebRtcControlChSize: len(s.webRtcControlCh),
		UnregisterChSize:    len(s.unregisterCh),
	}

	for i, p := range s.peers {
		report := p.Conn.GetStats()
		stats := PeerStats{
			StatsReport: report,
			Alias:       p.Alias,
		}
		serverStats.Peers[i] = stats
	}

	return serverStats
}
