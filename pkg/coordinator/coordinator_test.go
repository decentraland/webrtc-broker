package coordinator

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"

	_testing "github.com/decentraland/webrtc-broker/internal/testing"
	"github.com/decentraland/webrtc-broker/internal/ws"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

type MockWebsocket = _testing.MockWebsocket

type mockUpgrader struct {
	mock.Mock
}

func (m *mockUpgrader) Upgrade(w http.ResponseWriter, r *http.Request) (ws.IWebsocket, error) {
	args := m.Called(w, r)
	return args.Get(0).(ws.IWebsocket), args.Error(1)
}

type mockCoordinatorAuthenticator struct{ mock.Mock }

func (m *mockCoordinatorAuthenticator) AuthenticateFromURL(role protocol.Role, r *http.Request) (bool, error) {
	args := m.Called(role, r)
	return args.Bool(0), args.Error(1)
}

func makeDefaultServerSelector() *DefaultServerSelector {
	return &DefaultServerSelector{ServerAliases: make(map[uint64]bool)}
}

func makeTestState() *State {
	config := Config{ServerSelector: makeDefaultServerSelector()}
	return MakeState(&config)
}

func TestUpgradeRequest(t *testing.T) {
	testSuccessfulUpgrade := func(t *testing.T, req *http.Request, expectedRole protocol.Role) {
		auth := &mockCoordinatorAuthenticator{}
		auth.On("AuthenticateFromURL", expectedRole, mock.Anything).Return(true, nil).Once()

		config := Config{
			ServerSelector: makeDefaultServerSelector(),
			Auth:           auth,
		}

		ws := &MockWebsocket{}

		upgrader := &mockUpgrader{}
		state := MakeState(&config)
		state.upgrader = upgrader

		upgrader.On("Upgrade", nil, req).Return(ws, nil)

		_, err := UpgradeRequest(state, expectedRole, nil, req)
		require.NoError(t, err)

		upgrader.AssertExpectations(t)
	}

	t.Run("upgrade discover request", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/discover?method=fake", nil)
		require.NoError(t, err)
		testSuccessfulUpgrade(t, req, protocol.Role_COMMUNICATION_SERVER)
	})

	t.Run("upgrade connect request", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/connect?method=fake", nil)
		require.NoError(t, err)
		testSuccessfulUpgrade(t, req, protocol.Role_CLIENT)
	})

	t.Run("upgrade request (unauthorized)", func(t *testing.T) {
		auth := &mockCoordinatorAuthenticator{}
		auth.On("AuthenticateFromURL", protocol.Role_COMMUNICATION_SERVER, mock.Anything).Return(false, nil).Once()
		config := Config{
			ServerSelector: makeDefaultServerSelector(),
			Auth:           auth,
		}

		upgrader := &mockUpgrader{}
		state := MakeState(&config)
		state.upgrader = upgrader

		req, err := http.NewRequest("GET", "/discover?method=fake", nil)
		require.NoError(t, err)
		_, err = UpgradeRequest(state, protocol.Role_COMMUNICATION_SERVER, nil, req)
		require.Equal(t, err, ErrUnauthorized)
		upgrader.AssertExpectations(t)
	})
}

func TestReadPump(t *testing.T) {
	t.Run("webrtc message", func(t *testing.T) {
		state := makeTestState()
		defer closeState(state)

		conn := &MockWebsocket{}
		p := makePeer(state, conn, protocol.Role_COMMUNICATION_SERVER)
		p.Alias = 1

		msg := &protocol.WebRtcMessage{
			Type:    protocol.MessageType_WEBRTC_ANSWER,
			ToAlias: 2,
		}

		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		conn.
			On("Close").Return(nil).Once().
			On("ReadMessage").Return(encodedMsg, nil).Once().
			On("ReadMessage").Return([]byte{}, errors.New("stop")).Once().
			On("SetReadLimit", mock.Anything).Return(nil).Once().
			On("SetReadDeadline", mock.Anything).Return(nil).Once().
			On("SetPongHandler", mock.Anything).Once()

		go readPump(state, p)

		unregistered := <-state.unregister

		require.Equal(t, p, unregistered)
		require.Len(t, state.signalingQueue, 1)

		in := <-state.signalingQueue
		require.Equal(t, msg.Type, in.msgType)
		require.Equal(t, p, in.from, p)
		require.Equal(t, uint64(2), in.toAlias)

		require.NoError(t, proto.Unmarshal(in.bytes, msg))
		require.Equal(t, uint64(1), msg.FromAlias)

		conn.AssertExpectations(t)
	})

	t.Run("connect message (with alias)", func(t *testing.T) {
		state := makeTestState()
		defer closeState(state)

		conn := &MockWebsocket{}
		p := makePeer(state, conn, protocol.Role_COMMUNICATION_SERVER)
		p.Alias = 1

		msg := &protocol.ConnectMessage{
			Type:    protocol.MessageType_CONNECT,
			ToAlias: 2,
		}
		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		conn.
			On("Close").Return(nil).Once().
			On("ReadMessage").Return(encodedMsg, nil).Once().
			On("ReadMessage").Return([]byte{}, errors.New("stop")).Once().
			On("SetReadLimit", mock.Anything).Return(nil).Once().
			On("SetReadDeadline", mock.Anything).Return(nil).Once().
			On("SetPongHandler", mock.Anything).Once()

		go readPump(state, p)

		unregistered := <-state.unregister

		require.Equal(t, p, unregistered)
		require.Len(t, state.signalingQueue, 1)

		in := <-state.signalingQueue
		require.Equal(t, msg.Type, in.msgType)
		require.Equal(t, p, in.from)
		require.Equal(t, uint64(2), in.toAlias)
	})
}

func TestWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.ConnectMessage{})
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		state := makeTestState()
		defer closeState(state)

		conn := &MockWebsocket{}
		conn.
			On("WriteMessage", msg).Return(nil).Once().
			On("WriteMessage", msg).Return(errors.New("stop")).Once()
		p := makePeer(state, conn, protocol.Role_CLIENT)
		p.Alias = 1

		p.sendCh <- msg
		p.sendCh <- msg

		p.writePump(state)
		conn.AssertExpectations(t)
	})

	t.Run("first write error", func(t *testing.T) {
		state := makeTestState()
		defer closeState(state)

		conn := &MockWebsocket{}
		conn.
			On("WriteMessage", msg).Return(errors.New("error")).Once()

		p := makePeer(state, conn, protocol.Role_CLIENT)
		p.Alias = 1

		p.sendCh <- msg
		p.sendCh <- msg

		p.writePump(state)
		conn.AssertExpectations(t)
	})
}

func TestConnectCommServer(t *testing.T) {
	forRole := func(t *testing.T, role protocol.Role) {
		state := makeTestState()
		defer closeState(state)

		conn := &MockWebsocket{}
		conn.
			On("Close").Return(nil).Once().
			On("ReadMessage").Return([]byte{}, nil).Maybe().
			On("WriteCloseMessage").Return(nil).Maybe().
			On("SetReadLimit", mock.Anything).Return(nil).Maybe().
			On("SetReadDeadline", mock.Anything).Return(nil).Maybe().
			On("SetPongHandler", mock.Anything).Maybe()
		ConnectCommServer(state, conn, role)

		p := <-state.registerCommServer
		p.close()

		require.Equal(t, p.role, role)
		conn.AssertExpectations(t)
	}

	t.Run("communication server role", func(t *testing.T) {
		forRole(t, protocol.Role_COMMUNICATION_SERVER)
	})

	t.Run("communication server hub role", func(t *testing.T) {
		forRole(t, protocol.Role_COMMUNICATION_SERVER_HUB)
	})
}

func TestConnectClient(t *testing.T) {
	state := makeTestState()
	defer closeState(state)

	conn := &MockWebsocket{}
	conn.
		On("Close").Return(nil).Once().
		On("ReadMessage").Return([]byte{}, nil).Maybe().
		On("WriteCloseMessage").Return(nil).Maybe().
		On("SetReadLimit", mock.Anything).Return(nil).Maybe().
		On("SetReadDeadline", mock.Anything).Return(nil).Maybe().
		On("SetPongHandler", mock.Anything).Maybe()
	ConnectClient(state, conn)

	p := <-state.registerClient
	p.close()

	require.Equal(t, p.role, protocol.Role_CLIENT)
	conn.AssertExpectations(t)
}

func TestRegisterCommServer(t *testing.T) {
	state := makeTestState()
	defer closeState(state)

	conn := &MockWebsocket{}
	conn.On("Close").Return(nil).Once()
	s := makePeer(state, conn, protocol.Role_COMMUNICATION_SERVER)

	conn2 := &MockWebsocket{}
	conn2.On("Close").Return(nil).Once()
	s2 := makePeer(state, conn2, protocol.Role_COMMUNICATION_SERVER)

	state.registerCommServer <- s
	state.registerCommServer <- s2

	go Start(state)

	welcomeMessage := &protocol.WelcomeMessage{}

	bytes := <-s.sendCh
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	bytes = <-s2.sendCh
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	state.stop <- true

	s.close()
	s2.close()

	conn.AssertExpectations(t)
	conn2.AssertExpectations(t)
}

func TestRegisterClient(t *testing.T) {
	state := makeTestState()
	defer closeState(state)

	conn := &MockWebsocket{}
	conn.On("Close").Return(nil).Once()
	c := makePeer(state, conn, protocol.Role_CLIENT)

	conn2 := &MockWebsocket{}
	conn2.On("Close").Return(nil).Once()
	c2 := makePeer(state, conn2, protocol.Role_CLIENT)

	state.registerClient <- c
	state.registerClient <- c2

	go Start(state)

	welcomeMessage := &protocol.WelcomeMessage{}

	bytes := <-c.sendCh
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	bytes = <-c2.sendCh
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	state.stop <- true

	c.close()
	c2.close()

	conn.AssertExpectations(t)
	conn2.AssertExpectations(t)
}

func TestUnregister(t *testing.T) {
	selector := makeDefaultServerSelector()

	config := Config{ServerSelector: selector}
	state := MakeState(&config)
	state.unregister = make(chan *Peer)

	defer closeState(state)

	conn := &MockWebsocket{}
	s := makePeer(state, conn, protocol.Role_COMMUNICATION_SERVER)
	s.Alias = 1
	state.Peers[s.Alias] = s
	selector.ServerAliases[s.Alias] = true

	conn2 := &MockWebsocket{}
	s2 := makePeer(state, conn2, protocol.Role_COMMUNICATION_SERVER)
	s2.Alias = 2
	state.Peers[s2.Alias] = s2
	selector.ServerAliases[s2.Alias] = true

	go Start(state)
	state.unregister <- s
	state.unregister <- s2
	state.stop <- true

	require.Len(t, state.Peers, 0)
	require.Len(t, selector.ServerAliases, 0)
}

func TestSignaling(t *testing.T) {
	bytes, err := proto.Marshal(&protocol.WebRtcMessage{})
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		state := makeTestState()
		defer closeState(state)

		conn := &MockWebsocket{}
		p := makePeer(state, conn, protocol.Role_CLIENT)
		p.Alias = 1

		conn2 := &MockWebsocket{}
		p2 := makePeer(state, conn2, protocol.Role_CLIENT)
		p2.Alias = 2

		state.Peers[p.Alias] = p
		state.Peers[p2.Alias] = p2

		state.signalingQueue <- &inMessage{
			msgType: protocol.MessageType_WEBRTC_ANSWER,
			from:    p,
			bytes:   bytes,
			toAlias: p2.Alias,
		}

		state.signalingQueue <- &inMessage{
			msgType: protocol.MessageType_WEBRTC_ANSWER,
			from:    p2,
			bytes:   bytes,
			toAlias: p.Alias,
		}

		go Start(state)

		<-p.sendCh
		<-p2.sendCh

		state.stop <- true
	})

	t.Run("on peer not found", func(t *testing.T) {
		state := makeTestState()
		state.signalingQueue = make(chan *inMessage)
		defer closeState(state)

		conn := &MockWebsocket{}
		p := makePeer(state, conn, protocol.Role_CLIENT)
		p.Alias = 1

		state.Peers[p.Alias] = p

		go Start(state)

		state.signalingQueue <- &inMessage{
			msgType: protocol.MessageType_WEBRTC_ANSWER,
			from:    p,
			bytes:   bytes,
			toAlias: 2,
		}

		state.stop <- true
	})

	t.Run("on channel closed", func(t *testing.T) {
		state := makeTestState()
		state.signalingQueue = make(chan *inMessage)
		defer closeState(state)

		conn := &MockWebsocket{}
		p := makePeer(state, conn, protocol.Role_CLIENT)
		p.Alias = 1

		conn2 := &MockWebsocket{}
		conn2.On("Close").Return(nil).Once()
		p2 := makePeer(state, conn2, protocol.Role_CLIENT)
		p2.Alias = 2
		p2.close()

		state.Peers[p.Alias] = p
		state.Peers[p2.Alias] = p2

		go Start(state)

		state.signalingQueue <- &inMessage{
			msgType: protocol.MessageType_WEBRTC_ANSWER,
			from:    p,
			bytes:   bytes,
			toAlias: p2.Alias,
		}

		state.stop <- true
	})
}
