package coordinator

import (
	"errors"
	"net/http"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/internal/ws"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
)

const (
	reportPeriod   = 30 * time.Second
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 5000 // NOTE let's adjust this later
)

// ErrUnauthorized indicates that a peer is not authorized for the role
var ErrUnauthorized = errors.New("unauthorized")

// IServerSelector is in charge of tracking and processing the server list
type IServerSelector interface {
	ServerRegistered(server *Peer)
	ServerUnregistered(server *Peer)
	GetServerAliasList(forPeer *Peer) []uint64
}

// DefaultServerSelector is the default server selector
type DefaultServerSelector struct {
	ServerAliases map[uint64]bool
}

// ServerRegistered register a new server
func (r *DefaultServerSelector) ServerRegistered(server *Peer) {
	r.ServerAliases[server.Alias] = true
}

// ServerUnregistered removes an unregistered server from the list
func (r *DefaultServerSelector) ServerUnregistered(server *Peer) {
	delete(r.ServerAliases, server.Alias)
}

// GetServerAliasList returns a list of tracked server aliases
func (r *DefaultServerSelector) GetServerAliasList(forPeer *Peer) []uint64 {
	peers := make([]uint64, 0, len(r.ServerAliases))

	for alias := range r.ServerAliases {
		peers = append(peers, alias)
	}

	return peers
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
	isServer bool
}

// State represent the state of the coordinator
type State struct {
	serverSelector IServerSelector
	upgrader       ws.IUpgrader
	auth           authentication.CoordinatorAuthenticator
	marshaller     protocol.IMarshaller
	log            *logging.Logger

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
}

// MakeState creates a new CoordinatorState
func MakeState(config *Config) *State {
	serverSelector := config.ServerSelector
	if serverSelector == nil {
		serverSelector = &DefaultServerSelector{
			ServerAliases: make(map[uint64]bool),
		}
	}

	return &State{
		serverSelector:     serverSelector,
		upgrader:           ws.MakeUpgrader(),
		auth:               config.Auth,
		marshaller:         &protocol.Marshaller{},
		log:                config.Log,
		Peers:              make(map[uint64]*Peer),
		registerCommServer: make(chan *Peer, 255),
		registerClient:     make(chan *Peer, 255),
		unregister:         make(chan *Peer, 255),
		signalingQueue:     make(chan *inMessage, 255),
		stop:               make(chan bool),
	}
}

func makePeer(conn ws.IWebsocket, isServer bool) *Peer {
	return &Peer{
		conn:     conn,
		sendCh:   make(chan []byte, 256),
		isServer: isServer,
	}
}

func makeClient(conn ws.IWebsocket) *Peer     { return makePeer(conn, false) }
func makeCommServer(conn ws.IWebsocket) *Peer { return makePeer(conn, true) }

func (p *Peer) send(state *State, msg protocol.Message) error {
	log := state.log
	bytes, err := state.marshaller.Marshal(msg)
	if err != nil {
		log.WithError(err).Error("encode message failure")
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
			p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				p.conn.WriteCloseMessage()
				return
			}
			if err := p.conn.WriteMessage(bytes); err != nil {
				log.WithError(err).Error("error writing message")
				return
			}

			n := len(p.sendCh)
			for i := 0; i < n; i++ {
				bytes = <-p.sendCh
				if err := p.conn.WriteMessage(bytes); err != nil {
					log.WithError(err).Error("error writing message")
					return
				}
			}
		case <-ticker.C:
			p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := p.conn.WritePingMessage(); err != nil {
				log.WithError(err).Error("error writing ping message")
				return
			}
		}
	}
}

func (p *Peer) close() {
	if !p.isClosed {
		p.conn.Close()
		close(p.sendCh)
		p.isClosed = true
	}
}

func readPump(state *State, p *Peer) {
	defer func() {
		state.unregister <- p
		p.close()
	}()
	log := state.log
	marshaller := state.marshaller
	p.conn.SetReadLimit(maxMessageSize)
	p.conn.SetReadDeadline(time.Now().Add(pongWait))
	p.conn.SetPongHandler(func(s string) error {
		p.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	header := &protocol.CoordinatorMessage{}
	webRtcMessage := &protocol.WebRtcMessage{}
	connectMessage := &protocol.ConnectMessage{}

	for {
		bytes, err := p.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err) {
				log.WithError(err).Error("unexcepted close error")
			} else {
				log.WithError(err).Error("read error")
			}
			break
		}

		if err := marshaller.Unmarshal(bytes, header); err != nil {
			log.WithError(err).Debug("decode header failure")
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
				log.WithError(err).Debug("decode connect message failure")
				continue
			}

			connectMessage.FromAlias = p.Alias

			bytes, err := marshaller.Marshal(connectMessage)
			if err != nil {
				log.WithError(err).Error("cannot recode connect message")
				continue
			}

			state.signalingQueue <- &inMessage{
				msgType: msgType,
				from:    p,
				bytes:   bytes,
				toAlias: connectMessage.ToAlias,
			}
		default:
			log.WithField("type", msgType).Debug("unhandled message")
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
func ConnectCommServer(state *State, conn ws.IWebsocket) {
	log := state.log
	log.Info("socket connect (server)")
	p := makeCommServer(conn)
	state.registerCommServer <- p
	go readPump(state, p)
	go p.writePump(state)
}

// ConnectClient establish a ws connection to a client
func ConnectClient(state *State, conn ws.IWebsocket) {
	log := state.log
	log.Info("socket connect (client)")
	p := makeClient(conn)
	state.registerClient <- p
	go readPump(state, p)
	go p.writePump(state)
}

// Start starts the coordinator
func Start(state *State) {
	log := state.log
	ticker := time.NewTicker(reportPeriod)
	defer ticker.Stop()
	for {
		select {
		case s := <-state.registerCommServer:
			registerCommServer(state, s)
			n := len(state.registerCommServer)
			for i := 0; i < n; i++ {
				s = <-state.registerCommServer
				registerCommServer(state, s)
			}
		case c := <-state.registerClient:
			registerClient(state, c)
			n := len(state.registerClient)
			for i := 0; i < n; i++ {
				c = <-state.registerClient
				registerClient(state, c)
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
			clientsCount := 0
			serversCount := 0

			for _, p := range state.Peers {
				if p.isServer {
					serversCount++
				} else {
					clientsCount++
				}
			}

		case <-state.stop:
			log.Debug("stop signal")
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

// Register coordinator endpoints for server discovery and client connect
func Register(state *State, mux *http.ServeMux) {
	mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		ws, err := UpgradeRequest(state, protocol.Role_COMMUNICATION_SERVER, w, r)

		if err != nil {
			state.log.WithError(err).Error("socket connect error (discovery)")
			return
		}

		ConnectCommServer(state, ws)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		ws, err := UpgradeRequest(state, protocol.Role_CLIENT, w, r)

		if err != nil {
			state.log.WithError(err).Error("socket connect error (client)")
			return
		}

		ConnectClient(state, ws)
	})
}

func registerCommServer(state *State, p *Peer) error {
	state.LastPeerAlias++
	alias := state.LastPeerAlias
	p.Alias = alias

	servers := state.serverSelector.GetServerAliasList(p)

	state.Peers[alias] = p
	state.serverSelector.ServerRegistered(p)

	msg := &protocol.WelcomeMessage{
		Type:             protocol.MessageType_WELCOME,
		Alias:            alias,
		AvailableServers: servers,
	}

	return p.send(state, msg)
}

func registerClient(state *State, p *Peer) {
	state.LastPeerAlias++
	alias := state.LastPeerAlias
	p.Alias = alias

	servers := state.serverSelector.GetServerAliasList(p)

	state.Peers[alias] = p

	msg := &protocol.WelcomeMessage{
		Type:             protocol.MessageType_WELCOME,
		Alias:            alias,
		AvailableServers: servers,
	}
	p.send(state, msg)
}

func unregister(state *State, p *Peer) {
	delete(state.Peers, p.Alias)

	if p.isServer {
		state.serverSelector.ServerUnregistered(p)
	}
}

func signal(state *State, inMsg *inMessage) {
	toAlias := inMsg.toAlias
	p := state.Peers[toAlias]

	if p != nil && !p.isClosed {
		p.sendCh <- inMsg.bytes
	}
}

func repackageWebRtcMessage(state *State, from *Peer, bytes []byte, webRtcMessage *protocol.WebRtcMessage) ([]byte, error) {
	log := state.log
	marshaller := state.marshaller
	if err := marshaller.Unmarshal(bytes, webRtcMessage); err != nil {
		log.WithError(err).Debug("decode webrtc message failure")
		return nil, err
	}
	webRtcMessage.FromAlias = from.Alias

	bytes, err := marshaller.Marshal(webRtcMessage)
	if err != nil {
		log.WithError(err).Debug("encode message failure")
		return nil, err
	}

	return bytes, nil
}
