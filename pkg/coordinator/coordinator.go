package coordinator

import (
	"errors"
	"net/http"
	"time"

	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/ws"
	"github.com/decentraland/communications-server-go/pkg/authentication"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
)

const (
	reportPeriod   = 30 * time.Second
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 5000 // NOTE let's adjust this later
)

var UnauthorizedError = errors.New("unathorized")
var NoMethodProvidedError = errors.New("no method provided")

type inMessage struct {
	msgType protocol.MessageType
	from    *Peer
	bytes   []byte
	toAlias uint64
}

type Peer struct {
	Alias    uint64
	conn     ws.IWebsocket
	send     chan []byte
	isClosed bool
	isServer bool
}

// IServerSelector is in charge of tracking and processing the server list
type IServerSelector interface {
	ServerRegistered(server *Peer)
	ServerUnregistered(server *Peer)
	GetServerAliasList(forPeer *Peer) []uint64
}

// CoordinatorState represent the state of the coordinator
type CoordinatorState struct {
	serverSelector IServerSelector
	upgrader       ws.IUpgrader
	auth           authentication.Authentication
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
	Auth           authentication.Authentication
}

// MakeState creates a new CoordinatorState
func MakeState(config *Config) *CoordinatorState {
	return &CoordinatorState{
		serverSelector:     config.ServerSelector,
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
		send:     make(chan []byte, 256),
		isServer: isServer,
	}
}

func makeClient(conn ws.IWebsocket) *Peer     { return makePeer(conn, false) }
func makeCommServer(conn ws.IWebsocket) *Peer { return makePeer(conn, true) }

func (p *Peer) Send(state *CoordinatorState, msg protocol.Message) error {
	log := state.log
	bytes, err := state.marshaller.Marshal(msg)
	if err != nil {
		log.WithError(err).Error("encode message failure")
		return err
	}

	p.send <- bytes
	return nil
}

func (p *Peer) writePump(state *CoordinatorState) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
	}()

	log := state.log

	for {
		select {
		case bytes, ok := <-p.send:
			p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				p.conn.WriteCloseMessage()
				return
			}
			if err := p.conn.WriteMessage(bytes); err != nil {
				log.WithError(err).Error("error writing message")
				return
			}

			n := len(p.send)
			for i := 0; i < n; i++ {
				bytes = <-p.send
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

func (p *Peer) Close() {
	if !p.isClosed {
		p.conn.Close()
		close(p.send)
		p.isClosed = true
	}
}

func readPump(state *CoordinatorState, p *Peer) {
	defer func() {
		state.unregister <- p
		p.Close()
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

func UpgradeRequest(state *CoordinatorState, role protocol.Role, w http.ResponseWriter, r *http.Request) (ws.IWebsocket, error) {
	qs := r.URL.Query()

	method := qs["method"]

	if len(method) == 0 {
		return nil, NoMethodProvidedError
	}

	isValid, err := state.auth.AuthenticateQs(method[0], role, qs)

	if err != nil {
		return nil, err
	}

	if !isValid {
		return nil, UnauthorizedError
	}

	return state.upgrader.Upgrade(w, r)
}

func closeState(state *CoordinatorState) {
	close(state.registerClient)
	close(state.registerCommServer)
	close(state.unregister)
	close(state.signalingQueue)
	close(state.stop)
}

func ConnectCommServer(state *CoordinatorState, conn ws.IWebsocket) {
	log := state.log
	log.Info("socket connect (server)")
	p := makeCommServer(conn)
	state.registerCommServer <- p
	go readPump(state, p)
	go p.writePump(state)
}

func ConnectClient(state *CoordinatorState, conn ws.IWebsocket) {
	log := state.log
	log.Info("socket connect (client)")
	p := makeClient(conn)
	state.registerClient <- p
	go readPump(state, p)
	go p.writePump(state)
}

func Process(state *CoordinatorState) {
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
					serversCount += 1
				} else {
					clientsCount += 1
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
func Register(state *CoordinatorState, mux *http.ServeMux) {
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

func registerCommServer(state *CoordinatorState, p *Peer) error {
	state.LastPeerAlias += 1
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

	return p.Send(state, msg)
}

func registerClient(state *CoordinatorState, p *Peer) {
	state.LastPeerAlias += 1
	alias := state.LastPeerAlias
	p.Alias = alias

	servers := state.serverSelector.GetServerAliasList(p)

	state.Peers[alias] = p

	msg := &protocol.WelcomeMessage{
		Type:             protocol.MessageType_WELCOME,
		Alias:            alias,
		AvailableServers: servers,
	}
	p.Send(state, msg)
}

func unregister(state *CoordinatorState, p *Peer) {
	delete(state.Peers, p.Alias)

	if p.isServer {
		state.serverSelector.ServerUnregistered(p)
	}
}

func signal(state *CoordinatorState, inMsg *inMessage) {
	toAlias := inMsg.toAlias
	p := state.Peers[toAlias]

	if p != nil && !p.isClosed {
		p.send <- inMsg.bytes
	}
}

func repackageWebRtcMessage(state *CoordinatorState, from *Peer, bytes []byte, webRtcMessage *protocol.WebRtcMessage) ([]byte, error) {
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
