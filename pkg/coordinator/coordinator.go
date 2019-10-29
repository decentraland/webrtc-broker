// Package coordinator contains the coordinator definition
package coordinator

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/internal/ws"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

const (
	defaultReportPeriod = 30 * time.Second
	pongWait            = 60 * time.Second
	pingPeriod          = 30 * time.Second
	maxMessageSize      = 5000 // NOTE let's adjust this later
)

// Stats expose coordinator stats for reporting purposes
type Stats struct {
	ServerCount int
	ClientCount int
}

// ErrUnauthorized indicates that a peer is not authorized for the role
var ErrUnauthorized = errors.New("unauthorized")

// IServerSelector is in charge of tracking and processing the server list
type IServerSelector interface {
	ServerRegistered(role protocol.Role, alias uint64)
	ServerUnregistered(alias uint64)
	GetServerAliasList(forRole protocol.Role) []uint64
	GetServerCount() int
}

// DefaultServerSelector is the default server selector
type DefaultServerSelector struct {
	ServerAliases map[uint64]bool
}

// ServerRegistered register a new server
func (r *DefaultServerSelector) ServerRegistered(role protocol.Role, alias uint64) {
	r.ServerAliases[alias] = true
}

// ServerUnregistered removes an unregistered server from the list
func (r *DefaultServerSelector) ServerUnregistered(alias uint64) {
	delete(r.ServerAliases, alias)
}

// ByAlias is a utility to sort peers by alias
type ByAlias []uint64

func (a ByAlias) Len() int           { return len(a) }
func (a ByAlias) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAlias) Less(i, j int) bool { return a[i] < a[j] }

// GetServerAliasList returns a list of tracked server aliases
func (r *DefaultServerSelector) GetServerAliasList(role protocol.Role) []uint64 {
	peers := make([]uint64, 0, len(r.ServerAliases))

	for alias := range r.ServerAliases {
		peers = append(peers, alias)
	}

	sort.Sort(ByAlias(peers))

	return peers
}

// GetServerCount return amount of servers registered
func (r *DefaultServerSelector) GetServerCount() int {
	return len(r.ServerAliases)
}

type inMessage struct {
	msgType protocol.MessageType
	from    *Peer
	bytes   []byte
	toAlias uint64
}

// Peer represents any peer, both server and clients
type Peer struct {
	Alias    uint64
	conn     ws.IWebsocket
	sendCh   chan []byte
	isClosed bool
	role     protocol.Role
	log      logging.Logger
}

// State represent the state of the coordinator
type State struct {
	serverSelector IServerSelector
	upgrader       ws.IUpgrader
	auth           authentication.CoordinatorAuthenticator
	marshaller     protocol.IMarshaller
	log            logging.Logger
	reporter       func(stats Stats)
	reportPeriod   time.Duration

	LastPeerAlias uint64

	Peers              map[uint64]*Peer
	registerCommServer chan *Peer
	registerClient     chan *Peer
	unregister         chan *Peer
	signalingQueue     chan *inMessage
	stop               chan bool
	softStop           bool
}

// Config is the coordinator config
type Config struct {
	Log            *logging.Logger
	ServerSelector IServerSelector
	Auth           authentication.CoordinatorAuthenticator
	Reporter       func(stats Stats)
	ReportPeriod   time.Duration
}

// MakeState creates a new CoordinatorState
func MakeState(config *Config) *State {
	serverSelector := config.ServerSelector
	if serverSelector == nil {
		serverSelector = &DefaultServerSelector{
			ServerAliases: make(map[uint64]bool),
		}
	}

	reportPeriod := config.ReportPeriod
	if reportPeriod.Seconds() == 0 {
		reportPeriod = defaultReportPeriod
	}

	var log logging.Logger
	if config.Log == nil {
		log = logging.New()
	} else {
		log = *config.Log
	}

	return &State{
		serverSelector:     serverSelector,
		reporter:           config.Reporter,
		reportPeriod:       reportPeriod,
		upgrader:           ws.MakeUpgrader(),
		auth:               config.Auth,
		marshaller:         &protocol.Marshaller{},
		log:                log,
		Peers:              make(map[uint64]*Peer),
		registerCommServer: make(chan *Peer, 255),
		registerClient:     make(chan *Peer, 255),
		unregister:         make(chan *Peer, 255),
		signalingQueue:     make(chan *inMessage, 255),
		stop:               make(chan bool),
	}
}

func makePeer(state *State, conn ws.IWebsocket, role protocol.Role) *Peer {
	return &Peer{
		conn:   conn,
		sendCh: make(chan []byte, 256),
		role:   role,
		log:    state.log,
	}
}

func (p *Peer) send(state *State, msg protocol.Message) error {
	log := state.log

	bytes, err := state.marshaller.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("encode message failure")
		return err
	}

	p.sendCh <- bytes

	return nil
}

func (p *Peer) writePump(state *State) {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
	}()

	log := state.log

	for {
		select {
		case bytes, ok := <-p.sendCh:
			if !ok {
				if err := p.conn.WriteCloseMessage(); err != nil {
					log.Debug().Err(err).Msg("error writing close message")
				}

				return
			}

			if err := p.conn.WriteMessage(bytes); err != nil {
				log.Error().Err(err).Msg("error writing message")
				return
			}

			n := len(p.sendCh)
			for i := 0; i < n; i++ {
				bytes, ok = <-p.sendCh
				if !ok {
					if err := p.conn.WriteCloseMessage(); err != nil {
						log.Debug().Err(err).Msg("error writing close message")
					}

					return
				}

				if err := p.conn.WriteMessage(bytes); err != nil {
					log.Error().Err(err).Msg("error writing message")
					return
				}
			}
		case <-ticker.C:
			if err := p.conn.WritePingMessage(); err != nil {
				log.Error().Err(err).Msg("error writing ping message")
				return
			}
		}
	}
}

func (p *Peer) close() {
	if !p.isClosed {
		if err := p.conn.Close(); err != nil {
			p.log.Debug().Err(err).Msg("error closing peer")
		}

		close(p.sendCh)
		p.isClosed = true
	}
}

func readPump(state *State, p *Peer) {
	defer func() {
		p.close()
		state.unregister <- p
	}()

	log := state.log
	marshaller := state.marshaller

	p.conn.SetReadLimit(maxMessageSize)

	if err := p.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Error().Err(err).Msg("error setting read deadline")
		return
	}

	p.conn.SetPongHandler(func(s string) error {
		err := p.conn.SetReadDeadline(time.Now().Add(pongWait))
		return err
	})

	header := &protocol.CoordinatorMessage{}
	webRtcMessage := &protocol.WebRtcMessage{}
	connectMessage := &protocol.ConnectMessage{}
	connectionRefusedMessage := &protocol.ConnectionRefusedMessage{}

	for {
		bytes, err := p.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err) {
				log.Error().Err(err).Msg("unexcepted close error")
			} else {
				log.Error().Err(err).Msg("read error")
			}

			break
		}

		if err = marshaller.Unmarshal(bytes, header); err != nil {
			log.Debug().Err(err).Msg("decode header failure")
			continue
		}

		msgType := header.GetType()

		switch msgType {
		case protocol.MessageType_WEBRTC_OFFER, protocol.MessageType_WEBRTC_ANSWER, protocol.MessageType_WEBRTC_ICE_CANDIDATE:
			bytes, err = repackageWebRtcMessage(state, p, bytes, webRtcMessage)
			if err != nil {
				continue
			}

			state.signalingQueue <- &inMessage{
				msgType: msgType,
				from:    p,
				bytes:   bytes,
				toAlias: webRtcMessage.ToAlias,
			}
		case protocol.MessageType_CONNECT:
			if err := marshaller.Unmarshal(bytes, connectMessage); err != nil {
				log.Debug().Err(err).Msg("decode connect message failure")
				continue
			}

			connectMessage.FromAlias = p.Alias

			bytes, err := marshaller.Marshal(connectMessage)
			if err != nil {
				log.Error().Err(err).Msg("cannot recode connect message")
				continue
			}

			state.signalingQueue <- &inMessage{
				msgType: msgType,
				from:    p,
				bytes:   bytes,
				toAlias: connectMessage.ToAlias,
			}
		case protocol.MessageType_CONNECTION_REFUSED:
			if err := marshaller.Unmarshal(bytes, connectionRefusedMessage); err != nil {
				log.Debug().Err(err).Msg("decode connection refused connection message failure")
				continue
			}

			connectionRefusedMessage.FromAlias = p.Alias

			bytes, err := marshaller.Marshal(connectionRefusedMessage)
			if err != nil {
				log.Error().Err(err).Msg("cannot reencode refused connection message")
				continue
			}

			state.signalingQueue <- &inMessage{
				msgType: msgType,
				from:    p,
				bytes:   bytes,
				toAlias: connectionRefusedMessage.ToAlias,
			}
		default:
			log.Debug().Str("type", msgType.String()).Msg("unhandled message")
		}
	}
}

// UpgradeRequest upgrades a HTTP request to ws protocol and authenticates for the role
func UpgradeRequest(state *State, role protocol.Role, w http.ResponseWriter, r *http.Request) (ws.IWebsocket, error) {
	isValid, err := state.auth.AuthenticateFromURL(role, r)

	if err != nil {
		return nil, err
	}

	if !isValid {
		return nil, ErrUnauthorized
	}

	return state.upgrader.Upgrade(w, r)
}

func closeState(state *State) {
	close(state.registerClient)
	close(state.registerCommServer)
	close(state.unregister)
	close(state.signalingQueue)
	close(state.stop)
}

// ConnectCommServer establish a ws connection to a communication server
func ConnectCommServer(state *State, conn ws.IWebsocket, role protocol.Role) {
	log := state.log
	log.Info().Msg("socket connect (server)")

	p := makePeer(state, conn, role)
	state.registerCommServer <- p

	go readPump(state, p)

	go p.writePump(state)
}

// ConnectClient establish a ws connection to a client
func ConnectClient(state *State, conn ws.IWebsocket) {
	log := state.log
	log.Info().Msg("socket connect (client)")

	p := makePeer(state, conn, protocol.Role_CLIENT)
	state.registerClient <- p

	go readPump(state, p)

	go p.writePump(state)
}

// Start starts the coordinator
func Start(state *State) {
	log := state.log
	ticker := time.NewTicker(state.reportPeriod)

	defer ticker.Stop()

	ignoreError := func(err error) {
		if err != nil {
			log.Debug().Err(err).Msg("ignoring error")
		}
	}

	for {
		select {
		case s := <-state.registerCommServer:
			ignoreError(registerCommServer(state, s))

			n := len(state.registerCommServer)
			for i := 0; i < n; i++ {
				s = <-state.registerCommServer
				ignoreError(registerCommServer(state, s))
			}
		case c := <-state.registerClient:
			ignoreError(registerClient(state, c))

			n := len(state.registerClient)
			for i := 0; i < n; i++ {
				c = <-state.registerClient
				ignoreError(registerClient(state, c))
			}
		case c := <-state.unregister:
			unregister(state, c)

			n := len(state.unregister)
			for i := 0; i < n; i++ {
				c = <-state.unregister
				unregister(state, c)
			}
		case inMsg := <-state.signalingQueue:
			signal(state, inMsg)

			n := len(state.signalingQueue)
			for i := 0; i < n; i++ {
				inMsg = <-state.signalingQueue
				signal(state, inMsg)
			}
		case <-ticker.C:
			if state.reporter != nil {
				serverCount := state.serverSelector.GetServerCount()
				clientCount := len(state.Peers) - serverCount

				stats := Stats{
					ServerCount: serverCount,
					ClientCount: clientCount,
				}
				state.reporter(stats)
			}
		case <-state.stop:
			log.Debug().Msg("stop signal")
			return
		}

		// NOTE: I'm using this for testing only, but if it makes sense to fully support it
		// we may want to add a timeout (with a timer), otherwise this will executed only
		// if the previous select exited
		if state.softStop {
			log.Debug().Msg("soft stop signal")
			return
		}
	}
}

// Register coordinator endpoints for server discovery and client connect
func Register(state *State, mux *http.ServeMux) {
	mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		qs := r.URL.Query()
		role := protocol.Role_COMMUNICATION_SERVER

		if qs.Get("role") == protocol.Role_COMMUNICATION_SERVER_HUB.String() {
			role = protocol.Role_COMMUNICATION_SERVER_HUB
		}

		ws, err := UpgradeRequest(state, role, w, r)

		if err != nil {
			state.log.Error().Err(err).Msg("socket connect error (discovery)")
			return
		}

		ConnectCommServer(state, ws, role)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		ws, err := UpgradeRequest(state, protocol.Role_CLIENT, w, r)

		if err != nil {
			state.log.Error().Err(err).Msg("socket connect error (client)")
			return
		}

		ConnectClient(state, ws)
	})
}

func registerCommServer(state *State, p *Peer) error {
	state.LastPeerAlias++
	alias := state.LastPeerAlias
	p.Alias = alias

	servers := state.serverSelector.GetServerAliasList(p.role)

	state.Peers[alias] = p
	state.serverSelector.ServerRegistered(p.role, p.Alias)

	msg := &protocol.WelcomeMessage{
		Type:             protocol.MessageType_WELCOME,
		Alias:            alias,
		AvailableServers: servers,
	}

	return p.send(state, msg)
}

func registerClient(state *State, p *Peer) error {
	state.LastPeerAlias++
	alias := state.LastPeerAlias
	p.Alias = alias

	servers := state.serverSelector.GetServerAliasList(p.role)

	state.Peers[alias] = p

	msg := &protocol.WelcomeMessage{
		Type:             protocol.MessageType_WELCOME,
		Alias:            alias,
		AvailableServers: servers,
	}

	if err := p.send(state, msg); err != nil {
		p.close()
		return err
	}

	return nil
}

func unregister(state *State, p *Peer) {
	delete(state.Peers, p.Alias)

	switch p.role {
	case protocol.Role_CLIENT:
	case protocol.Role_COMMUNICATION_SERVER:
		state.serverSelector.ServerUnregistered(p.Alias)
	case protocol.Role_COMMUNICATION_SERVER_HUB:
		state.serverSelector.ServerUnregistered(p.Alias)
	default:
		panic("unhandled role in unregister")
	}
}

func signal(state *State, inMsg *inMessage) {
	toAlias := inMsg.toAlias
	p := state.Peers[toAlias]

	if p != nil && !p.isClosed {
		p.sendCh <- inMsg.bytes
	}
}

func repackageWebRtcMessage(state *State, from *Peer, bytes []byte,
	webRtcMessage *protocol.WebRtcMessage) ([]byte, error) {
	log := state.log
	marshaller := state.marshaller

	if err := marshaller.Unmarshal(bytes, webRtcMessage); err != nil {
		log.Debug().Err(err).Msg("decode webrtc message failure")
		return nil, err
	}

	webRtcMessage.FromAlias = from.Alias

	bytes, err := marshaller.Marshal(webRtcMessage)
	if err != nil {
		log.Debug().Err(err).Msg("encode message failure")
		return nil, err
	}

	return bytes, nil
}
