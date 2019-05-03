package coordinator

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/sirupsen/logrus"

	_testing "github.com/decentraland/webrtc-broker/internal/testing"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

var appName = "coordinator-test"

type MockWebsocket = _testing.MockWebsocket
type MockUpgrader = _testing.MockUpgrader
type MockAuthenticator = _testing.MockAuthenticator

func makeMockWebsocket() *MockWebsocket {
	return _testing.MakeMockWebsocket()
}

func makeTestState(t *testing.T) *CoordinatorState {
	config := Config{
		ServerSelector: MakeRandomServerSelector(),
		Log:            logrus.New(),
	}
	return MakeState(&config)
}

func TestUpgradeRequest(t *testing.T) {

	testSuccessfulUpgrade := func(t *testing.T, req *http.Request, expectedRole protocol.Role) {
		auth := authentication.Make()
		auth.AddOrUpdateAuthenticator("fake", &MockAuthenticator{
			AuthenticateQs_: func(role protocol.Role, qs url.Values) (bool, error) {
				require.Equal(t, role, expectedRole)
				return true, nil
			},
		})
		config := Config{
			ServerSelector: MakeRandomServerSelector(),
			Auth:           auth,
		}

		ws := makeMockWebsocket()

		upgrader := &MockUpgrader{}
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
		auth := authentication.Make()
		auth.AddOrUpdateAuthenticator("fake", &MockAuthenticator{
			AuthenticateQs_: func(role protocol.Role, qs url.Values) (bool, error) {
				require.Equal(t, role, protocol.Role_COMMUNICATION_SERVER)
				return false, nil
			},
		})
		config := Config{
			ServerSelector: MakeRandomServerSelector(),
			Auth:           auth,
		}

		upgrader := &MockUpgrader{}
		state := MakeState(&config)
		state.upgrader = upgrader

		req, err := http.NewRequest("GET", "/discover?method=fake", nil)
		require.NoError(t, err)
		_, err = UpgradeRequest(state, protocol.Role_COMMUNICATION_SERVER, nil, req)
		require.Equal(t, err, UnauthorizedError)
		upgrader.AssertExpectations(t)
	})

	t.Run("upgrade request (no method provided))", func(t *testing.T) {
		config := Config{
			ServerSelector: MakeRandomServerSelector(),
		}

		upgrader := &MockUpgrader{}
		state := MakeState(&config)
		state.upgrader = upgrader

		req, err := http.NewRequest("GET", "/discover", nil)
		require.NoError(t, err)
		_, err = UpgradeRequest(state, protocol.Role_COMMUNICATION_SERVER, nil, req)
		require.Equal(t, err, NoMethodProvidedError)
		upgrader.AssertExpectations(t)
	})
}

func TestReadPump(t *testing.T) {
	setup := func() (*CoordinatorState, *MockWebsocket, *Peer) {
		state := makeTestState(t)

		conn := makeMockWebsocket()
		p := makeCommServer(conn)
		p.Alias = 1
		return state, conn, p
	}

	t.Run("webrtc message", func(t *testing.T) {
		state, conn, client := setup()
		defer closeState(state)

		msg := &protocol.WebRtcMessage{
			Type:    protocol.MessageType_WEBRTC_ANSWER,
			ToAlias: 2,
		}
		require.NoError(t, conn.PrepareToRead(msg))
		go readPump(state, client)

		p := <-state.unregister

		require.Equal(t, client, p)
		require.Len(t, state.signalingQueue, 1)

		in := <-state.signalingQueue
		require.Equal(t, msg.Type, in.msgType)
		require.Equal(t, p, in.from, p)
		require.Equal(t, uint64(2), in.toAlias)

		require.NoError(t, proto.Unmarshal(in.bytes, msg))
		require.Equal(t, uint64(1), msg.FromAlias)
	})

	t.Run("connect message (with alias)", func(t *testing.T) {
		state, conn, client := setup()
		defer closeState(state)

		msg := &protocol.ConnectMessage{
			Type:    protocol.MessageType_CONNECT,
			ToAlias: 2,
		}
		require.NoError(t, conn.PrepareToRead(msg))
		go readPump(state, client)

		p := <-state.unregister

		require.Equal(t, client, p)
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
		state := makeTestState(t)
		defer closeState(state)

		conn := makeMockWebsocket()
		p := makeClient(conn)
		p.Alias = 1
		i := 0
		conn.OnWrite = func(bytes []byte) {
			require.Equal(t, bytes, msg)
			if i == 1 {
				p.Close()
				return
			}
			i += 1
		}

		p.send <- msg
		p.send <- msg

		p.writePump(state)
		require.Equal(t, i, 1)
	})

	t.Run("first write error", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(state)

		conn := makeMockWebsocket()
		conn.WriteMessage_ = func(ws *MockWebsocket, bytes []byte) error {
			return errors.New("test error on write")
		}
		p := makeClient(conn)
		p.Alias = 1

		p.send <- msg
		p.send <- msg

		p.writePump(state)
	})

	t.Run("first write error", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(state)

		conn := makeMockWebsocket()
		i := 0
		conn.WriteMessage_ = func(ws *MockWebsocket, bytes []byte) error {
			if i == 0 {
				i += 1
				return nil
			}
			return errors.New("test error on write")
		}
		p := makeClient(conn)
		p.Alias = 1

		p.send <- msg
		p.send <- msg

		p.writePump(state)
	})
}

func TestConnectCommServer(t *testing.T) {
	state := makeTestState(t)
	defer closeState(state)

	conn := makeMockWebsocket()
	ConnectCommServer(state, conn)

	p := <-state.registerCommServer
	p.Close()

	require.Equal(t, p.isServer, true)
}

func TestConnectClient(t *testing.T) {
	state := makeTestState(t)
	defer closeState(state)

	conn := makeMockWebsocket()
	ConnectClient(state, conn)

	p := <-state.registerClient
	p.Close()

	require.Equal(t, p.isServer, false)
}

func TestRegisterCommServer(t *testing.T) {
	state := makeTestState(t)
	defer closeState(state)

	conn := makeMockWebsocket()
	s := makeCommServer(conn)
	defer s.Close()

	conn2 := makeMockWebsocket()
	s2 := makeCommServer(conn2)
	defer s2.Close()

	state.registerCommServer <- s
	state.registerCommServer <- s2
	go Process(state)

	welcomeMessage := &protocol.WelcomeMessage{}

	bytes := <-s.send
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	bytes = <-s2.send
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	state.stop <- true
}

func TestRegisterClient(t *testing.T) {
	state := makeTestState(t)
	defer closeState(state)

	conn := makeMockWebsocket()
	c := makeClient(conn)
	defer c.Close()

	conn2 := makeMockWebsocket()
	c2 := makeClient(conn2)
	defer c2.Close()

	state.registerClient <- c
	state.registerClient <- c2
	go Process(state)

	welcomeMessage := &protocol.WelcomeMessage{}

	bytes := <-c.send
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	bytes = <-c2.send
	require.NoError(t, proto.Unmarshal(bytes, welcomeMessage))
	require.Equal(t, welcomeMessage.Type, protocol.MessageType_WELCOME)
	require.NotEmpty(t, welcomeMessage.Alias)

	state.stop <- true
}

func TestUnregister(t *testing.T) {
	selector := MakeRandomServerSelector()

	config := Config{
		ServerSelector: selector,
		Log:            logrus.New(),
	}
	state := MakeState(&config)
	state.unregister = make(chan *Peer)
	defer closeState(state)

	conn := makeMockWebsocket()
	s := makeCommServer(conn)
	s.Alias = 1
	defer s.Close()
	state.Peers[s.Alias] = s
	selector.serverAliases[s.Alias] = true

	conn2 := makeMockWebsocket()
	s2 := makeCommServer(conn2)
	s2.Alias = 2
	defer s.Close()
	state.Peers[s2.Alias] = s2
	selector.serverAliases[s2.Alias] = true

	go Process(state)
	state.unregister <- s
	state.unregister <- s2
	state.stop <- true

	require.Len(t, state.Peers, 0)
	require.Len(t, selector.serverAliases, 0)
}

func TestSignaling(t *testing.T) {
	bytes, err := proto.Marshal(&protocol.WebRtcMessage{})
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(state)

		conn := makeMockWebsocket()
		p := makeClient(conn)
		p.Alias = 1
		defer p.Close()

		conn2 := makeMockWebsocket()
		p2 := makeClient(conn2)
		p2.Alias = 2
		defer p2.Close()

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

		go Process(state)

		<-p.send
		<-p2.send

		state.stop <- true
	})

	t.Run("on peer not found", func(t *testing.T) {
		state := makeTestState(t)
		state.signalingQueue = make(chan *inMessage)
		defer closeState(state)

		conn := makeMockWebsocket()
		p := makeClient(conn)
		p.Alias = 1
		defer p.Close()

		state.Peers[p.Alias] = p

		go Process(state)

		state.signalingQueue <- &inMessage{
			msgType: protocol.MessageType_WEBRTC_ANSWER,
			from:    p,
			bytes:   bytes,
			toAlias: 2,
		}

		state.stop <- true
	})

	t.Run("on channel closed", func(t *testing.T) {
		state := makeTestState(t)
		state.signalingQueue = make(chan *inMessage)
		defer closeState(state)

		conn := makeMockWebsocket()
		p := makeClient(conn)
		p.Alias = 1
		defer p.Close()

		conn2 := makeMockWebsocket()
		p2 := makeClient(conn2)
		p2.Alias = 2
		p2.Close()

		state.Peers[p.Alias] = p
		state.Peers[p2.Alias] = p2

		go Process(state)

		state.signalingQueue <- &inMessage{
			msgType: protocol.MessageType_WEBRTC_ANSWER,
			from:    p,
			bytes:   bytes,
			toAlias: p2.Alias,
		}

		state.stop <- true
	})
}
