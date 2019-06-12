package commserver

import (
	"errors"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v2"
	"github.com/stretchr/testify/mock"

	_testing "github.com/decentraland/webrtc-broker/internal/testing"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type MockWebsocket = _testing.MockWebsocket

type mockServerAuthenticator struct{ mock.Mock }

func (m *mockServerAuthenticator) AuthenticateFromMessage(role protocol.Role, bytes []byte) (bool, error) {
	args := m.Called(role, bytes)
	return args.Bool(0), args.Error(1)
}

func (m *mockServerAuthenticator) GenerateServerAuthMessage() (*protocol.AuthMessage, error) {
	args := m.Called()
	return args.Get(0).(*protocol.AuthMessage), args.Error(1)
}

func (m *mockServerAuthenticator) GenerateServerConnectURL(coordinatorURL string) (string, error) {
	args := m.Called(coordinatorURL)
	return args.String(0), args.Error(1)
}

type mockReadWriteCloser struct {
	mock.Mock
}

func (m *mockReadWriteCloser) ReadDataChannel(p []byte) (int, bool, error) {
	n, err := m.Read(p)
	return n, false, err
}

func (m *mockReadWriteCloser) WriteDataChannel(p []byte, isString bool) (int, error) {
	return m.Write(p)
}

func (m *mockReadWriteCloser) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *mockReadWriteCloser) Write(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *mockReadWriteCloser) Close() error {
	args := m.Called()
	return args.Error(0)
}

type mockWebRtc struct {
	mock.Mock
}

func (m *mockWebRtc) newConnection(peerAlias uint64) (*PeerConnection, error) {
	args := m.Called(peerAlias)
	return args.Get(0).(*PeerConnection), args.Error(1)
}

func (m *mockWebRtc) createReliableDataChannel(conn *PeerConnection) (*DataChannel, error) {
	args := m.Called(conn)
	return args.Get(0).(*DataChannel), args.Error(1)
}

func (m *mockWebRtc) detach(dc *DataChannel) (ReadWriteCloser, error) {
	args := m.Called(dc)
	return args.Get(0).(ReadWriteCloser), args.Error(1)
}

func (m *mockWebRtc) createUnreliableDataChannel(conn *PeerConnection) (*DataChannel, error) {
	args := m.Called(conn)
	return args.Get(0).(*DataChannel), args.Error(1)
}

func (m *mockWebRtc) registerOpenHandler(dc *DataChannel, handler func()) {
	m.Called(dc, handler)
}

func (m *mockWebRtc) invokeOpenHandler(dc *DataChannel) {
	m.Called(dc)
}

func (m *mockWebRtc) createOffer(conn *PeerConnection) (string, error) {
	args := m.Called(conn)
	return args.String(0), args.Error(1)
}

func (m *mockWebRtc) onAnswer(conn *PeerConnection, sdp string) error {
	args := m.Called(conn, sdp)
	return args.Error(0)
}

func (m *mockWebRtc) onOffer(conn *PeerConnection, sdp string) (string, error) {
	args := m.Called(conn, sdp)
	return args.String(0), args.Error(1)
}

func (m *mockWebRtc) onIceCandidate(conn *PeerConnection, sdp string) error {
	args := m.Called(conn, sdp)
	return args.Error(0)
}

func (m *mockWebRtc) isClosed(conn *PeerConnection) bool {
	args := m.Called(conn)
	return args.Bool(0)
}

func (m *mockWebRtc) isNew(conn *PeerConnection) bool {
	args := m.Called(conn)
	return args.Bool(0)
}

func (m *mockWebRtc) close(conn *PeerConnection) error {
	args := m.Called(conn)
	return args.Error(0)
}

func makeDefaultMockWebRtc() *mockWebRtc {
	mockWebRtc := &mockWebRtc{}
	mockWebRtc.
		On("close", mock.Anything).Return(nil).Once().
		On("close", mock.Anything).Return(errors.New("already closed"))
	return mockWebRtc
}

func makeTestServicesWithWebRtc(t *testing.T, webRtc *mockWebRtc) services {
	services := services{
		Marshaller: &protocol.Marshaller{},
		WebRtc:     webRtc,
		Log:        logrus.New(),
		Zipper:     &GzipCompression{},
	}
	return services
}

func makeTestServices(t *testing.T) services {
	return makeTestServicesWithWebRtc(t, makeDefaultMockWebRtc())
}

func makeTestConfigWithWebRtc(t *testing.T, auth *mockServerAuthenticator, webRtc *mockWebRtc) *Config {
	logger := logrus.New()
	config := &Config{
		Auth:                    auth,
		EstablishSessionTimeout: 1 * time.Second,
		WebRtc:                  webRtc,
		Log:                     logger,
	}

	return config
}

func makeTestConfig(t *testing.T) *Config {
	return makeTestConfigWithWebRtc(t, nil, makeDefaultMockWebRtc())
}

func makeTestState(t *testing.T, config *Config) *State {
	state, err := MakeState(config)
	require.NoError(t, err)
	return state
}

func makeClient(alias uint64, services services) *peer {
	return &peer{
		services: services,
		Alias:    alias,
		conn:     &pion.PeerConnection{},
		Topics:   make(map[string]struct{}),
		role:     protocol.Role_CLIENT,
	}
}

func makeServer(alias uint64, services services) *peer {
	return &peer{
		services: services,
		Alias:    alias,
		conn:     &pion.PeerConnection{},
		Topics:   make(map[string]struct{}),
		role:     protocol.Role_COMMUNICATION_SERVER,
	}
}

func addPeer(state *State, p *peer) *peer {
	state.peers = append(state.peers, p)
	return p
}

func TestCoordinatorSend(t *testing.T) {
	config := makeTestConfig(t)
	state := makeTestState(t, config)
	c := coordinator{send: make(chan []byte, 256), log: logrus.New()}
	defer c.Close()

	msg1 := &protocol.PingMessage{}
	encoded1, err := proto.Marshal(msg1)
	require.NoError(t, err)
	require.NoError(t, c.Send(state, msg1))
	require.Len(t, c.send, 1)

	msg2 := &protocol.PingMessage{}
	encoded2, err := proto.Marshal(msg2)
	require.NoError(t, err)
	require.NoError(t, c.Send(state, msg2))
	require.Len(t, c.send, 2)

	require.Equal(t, <-c.send, encoded2)
	require.Equal(t, <-c.send, encoded1)
}

func TestCoordinatorReadPump(t *testing.T) {
	setup := func() *State {
		auth := &mockServerAuthenticator{}
		auth.On("GenerateServerAuthMessage").Return(&protocol.AuthMessage{}, nil).Once()

		config := makeTestConfig(t)
		config.Auth = auth
		state := makeTestState(t, config)

		return state
	}

	t.Run("welcome server message", func(t *testing.T) {
		state := setup()
		conn := &MockWebsocket{}
		state.coordinator.conn = conn
		msg := &protocol.WelcomeMessage{
			Type:             protocol.MessageType_WELCOME,
			Alias:            3,
			AvailableServers: []uint64{1, 2},
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

		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go state.coordinator.readPump(state, welcomeChannel)

		welcomeMessage := <-welcomeChannel
		<-state.stop

		require.Equal(t, uint64(3), welcomeMessage.Alias)
	})

	t.Run("webrtc message", func(t *testing.T) {
		state := setup()
		conn := &MockWebsocket{}
		state.coordinator.conn = conn
		msg := &protocol.WebRtcMessage{
			Type: protocol.MessageType_WEBRTC_ANSWER,
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
		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go state.coordinator.readPump(state, welcomeChannel)

		<-state.stop
		require.Len(t, state.webRtcControlQueue, 1)
	})

	t.Run("connect message", func(t *testing.T) {
		state := setup()
		conn := &MockWebsocket{}
		state.coordinator.conn = conn
		msg := &protocol.ConnectMessage{
			Type:      protocol.MessageType_CONNECT,
			FromAlias: 2,
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
		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go state.coordinator.readPump(state, welcomeChannel)

		<-state.stop
		require.Len(t, state.connectQueue, 1)
		require.Equal(t, uint64(2), <-state.connectQueue)
	})
}

func TestCoordinatorWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.PingMessage{})
	require.NoError(t, err)

	config := makeTestConfig(t)
	state := makeTestState(t, config)
	conn := &MockWebsocket{}
	conn.
		On("Close").Return(nil).Once().
		On("WriteMessage", msg).Return(nil).Once().
		On("WriteMessage", msg).Return(errors.New("stop")).Once().
		On("SetWriteDeadline", mock.Anything).Return(nil).Once()

	state.coordinator.conn = conn

	state.coordinator.send <- msg
	state.coordinator.send <- msg

	state.coordinator.writePump(state)
	conn.AssertExpectations(t)
}

func TestTopicSubscriptions(t *testing.T) {
	t.Run("add client subscription", func(t *testing.T) {
		services := makeTestServices(t)
		c1 := makeClient(1, services)
		c2 := makeClient(2, services)
		subscriptions := make(topicSubscriptions)

		require.True(t, subscriptions.AddClientSubscription("topic1", c1))

		require.Contains(t, subscriptions, "topic1")
		require.Contains(t, subscriptions["topic1"].clients, c1)
		require.Len(t, subscriptions["topic1"].clients, 1)
		require.Len(t, subscriptions["topic1"].servers, 0)

		require.False(t, subscriptions.AddClientSubscription("topic1", c2))

		require.Contains(t, subscriptions, "topic1")
		require.Contains(t, subscriptions["topic1"].clients, c1)
		require.Contains(t, subscriptions["topic1"].clients, c2)
		require.Len(t, subscriptions["topic1"].clients, 2)
		require.Len(t, subscriptions["topic1"].servers, 0)
	})

	t.Run("add server subscription", func(t *testing.T) {
		services := makeTestServices(t)
		s1 := makeServer(1, services)
		s2 := makeServer(2, services)
		subscriptions := make(topicSubscriptions)

		require.True(t, subscriptions.AddServerSubscription("topic1", s1))

		require.Contains(t, subscriptions, "topic1")
		require.Contains(t, subscriptions["topic1"].servers, s1)
		require.Len(t, subscriptions["topic1"].clients, 0)
		require.Len(t, subscriptions["topic1"].servers, 1)

		require.False(t, subscriptions.AddServerSubscription("topic1", s2))

		require.Contains(t, subscriptions, "topic1")
		require.Contains(t, subscriptions["topic1"].servers, s1)
		require.Contains(t, subscriptions["topic1"].servers, s2)
		require.Len(t, subscriptions["topic1"].clients, 0)
		require.Len(t, subscriptions["topic1"].servers, 2)
	})

	t.Run("remove client subscription", func(t *testing.T) {
		services := makeTestServices(t)
		c1 := makeClient(1, services)
		c2 := makeClient(2, services)
		subscriptions := make(topicSubscriptions)
		subscriptions["topic1"] = &topicSubscription{
			clients: []*peer{c1, c2},
			servers: make([]*peer, 0),
		}

		require.False(t, subscriptions.RemoveClientSubscription("topic1", c1))

		require.Contains(t, subscriptions, "topic1")
		require.Contains(t, subscriptions["topic1"].clients, c2)
		require.Len(t, subscriptions["topic1"].clients, 1)
		require.Len(t, subscriptions["topic1"].servers, 0)

		require.True(t, subscriptions.RemoveClientSubscription("topic1", c2))

		require.NotContains(t, subscriptions, "topic1")
	})

	t.Run("remove server subscription", func(t *testing.T) {
		services := makeTestServices(t)
		s1 := makeServer(1, services)
		s2 := makeServer(2, services)
		subscriptions := make(topicSubscriptions)
		subscriptions["topic1"] = &topicSubscription{
			clients: make([]*peer, 0),
			servers: []*peer{s1, s2},
		}

		require.False(t, subscriptions.RemoveServerSubscription("topic1", s1))

		require.Contains(t, subscriptions, "topic1")
		require.Contains(t, subscriptions["topic1"].servers, s2)
		require.Len(t, subscriptions["topic1"].clients, 0)
		require.Len(t, subscriptions["topic1"].servers, 1)

		require.True(t, subscriptions.RemoveServerSubscription("topic1", s2))

		require.NotContains(t, subscriptions, "topic1")
	})

	t.Run("remove client subscription, but server left", func(t *testing.T) {
		services := makeTestServices(t)
		c1 := makeClient(1, services)
		s1 := makeServer(2, services)
		subscriptions := make(topicSubscriptions)
		subscriptions["topic1"] = &topicSubscription{
			clients: []*peer{c1},
			servers: []*peer{s1},
		}

		require.False(t, subscriptions.RemoveClientSubscription("topic1", c1))

		require.Contains(t, subscriptions, "topic1")
		require.Len(t, subscriptions["topic1"].clients, 0)
		require.Len(t, subscriptions["topic1"].servers, 1)

		require.True(t, subscriptions.RemoveServerSubscription("topic1", s1))

		require.NotContains(t, subscriptions, "topic1")
	})

	t.Run("remove server subscription, but client left", func(t *testing.T) {
		services := makeTestServices(t)
		c1 := makeClient(1, services)
		s1 := makeServer(2, services)
		subscriptions := make(topicSubscriptions)
		subscriptions["topic1"] = &topicSubscription{
			clients: []*peer{c1},
			servers: []*peer{s1},
		}

		require.False(t, subscriptions.RemoveServerSubscription("topic1", s1))

		require.Contains(t, subscriptions, "topic1")
		require.Len(t, subscriptions["topic1"].clients, 1)
		require.Len(t, subscriptions["topic1"].servers, 0)

		require.True(t, subscriptions.RemoveClientSubscription("topic1", c1))

		require.NotContains(t, subscriptions, "topic1")
	})
}

type authExchangeTestConfig struct {
	t                *testing.T
	config           *Config
	firstMessageRecv protocol.Message
}

type authExchangeTest struct {
	t             *testing.T
	state         *State
	ReliableDC    *DataChannel
	UnreliableDC  *DataChannel
	ReliableRWC   *mockReadWriteCloser
	UnreliableRWC *mockReadWriteCloser
}

func setupAuthExchangeTest(config authExchangeTestConfig) *authExchangeTest {
	test := &authExchangeTest{t: config.t}

	encodedMsg, err := proto.Marshal(config.firstMessageRecv)
	require.NoError(config.t, err)

	test.ReliableRWC = &mockReadWriteCloser{}
	test.ReliableRWC.
		On("Write", mock.Anything).Return(0, nil).
		On("Read", mock.Anything).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]byte)
		copy(arg, encodedMsg)
	}).Return(len(encodedMsg), nil).Once().
		On("Read", mock.Anything).Return(0, errors.New("stop"))

	test.UnreliableRWC = &mockReadWriteCloser{}
	test.UnreliableRWC.
		On("Write", mock.Anything).Return(0, nil).
		On("Read", mock.Anything).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]byte)
		copy(arg, encodedMsg)
	}).Return(len(encodedMsg), nil).Once().
		On("Read", mock.Anything).Return(0, errors.New("stop"))

	return test
}

func TestInitPeer(t *testing.T) {
	t.Run("if no connection is establish eventually the peer is unregistered", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(true).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", mock.Anything, mock.Anything).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		state := makeTestState(t, config)
		defer closeState(state)

		_, err := initPeer(state, 1, protocol.Role_UNKNOWN_ROLE)
		require.NoError(t, err)

		p := <-state.unregisterQueue
		require.Equal(t, uint64(1), p.Alias)
	})

	t.Run("auth exchange: first message is not auth", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		auth := &mockServerAuthenticator{}
		auth.On("AuthenticateFromMessage", mock.Anything, mock.Anything).Return(true, nil)

		config := makeTestConfigWithWebRtc(t, auth, webRtc)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:      t,
			config: config,
			firstMessageRecv: &protocol.TopicMessage{
				Type: protocol.MessageType_TOPIC,
			},
		})

		state := makeTestState(test.t, config)
		defer closeState(state)

		var reliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(false).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("registerOpenHandler", unreliableDC, mock.Anything).Once().
			On("detach", reliableDC).Return(test.ReliableRWC, nil).
			On("detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		_, err := initPeer(state, 1, protocol.Role_UNKNOWN_ROLE)
		require.NoError(t, err)

		reliableOpenHandler()

		<-state.unregisterQueue
	})

	t.Run("auth exchange: invalid role received in auth message", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		auth := &mockServerAuthenticator{}
		config := makeTestConfigWithWebRtc(t, auth, webRtc)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:      t,
			config: config,
			firstMessageRecv: &protocol.AuthMessage{
				Type: protocol.MessageType_AUTH,
				Role: protocol.Role_UNKNOWN_ROLE,
			},
		})

		state := makeTestState(test.t, config)
		defer closeState(state)

		var reliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(false).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("registerOpenHandler", unreliableDC, mock.Anything).Once().
			On("detach", reliableDC).Return(test.ReliableRWC, nil).
			On("detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		_, err := initPeer(state, 1, protocol.Role_UNKNOWN_ROLE)
		require.NoError(t, err)

		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
	})

	t.Run("auth exchange: invalid credentials received", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		auth := &mockServerAuthenticator{}
		auth.On("AuthenticateFromMessage", mock.Anything, mock.Anything).Return(false, nil)
		config := makeTestConfigWithWebRtc(t, auth, webRtc)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:      t,
			config: config,
			firstMessageRecv: &protocol.AuthMessage{
				Type: protocol.MessageType_AUTH,
				Role: protocol.Role_CLIENT,
			},
		})

		state := makeTestState(test.t, config)
		defer closeState(state)

		var reliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(false).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("registerOpenHandler", unreliableDC, mock.Anything).Once().
			On("detach", reliableDC).Return(test.ReliableRWC, nil).
			On("detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		_, err := initPeer(state, 1, protocol.Role_UNKNOWN_ROLE)
		require.NoError(t, err)

		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
	})

	t.Run("auth exchange: valid credentials are received from a client", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		auth := &mockServerAuthenticator{}
		auth.On("AuthenticateFromMessage", mock.Anything, mock.Anything).Return(true, nil)
		config := makeTestConfigWithWebRtc(t, auth, webRtc)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:      t,
			config: config,
			firstMessageRecv: &protocol.AuthMessage{
				Type: protocol.MessageType_AUTH,
				Role: protocol.Role_CLIENT,
			},
		})

		state := makeTestState(test.t, config)
		defer closeState(state)

		var reliableOpenHandler func()
		var unreliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(false).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("registerOpenHandler", unreliableDC, mock.Anything).Run(func(args mock.Arguments) {
			unreliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("detach", reliableDC).Return(test.ReliableRWC, nil).
			On("detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		p, err := initPeer(state, 1, protocol.Role_UNKNOWN_ROLE)
		require.NoError(t, err)

		go unreliableOpenHandler()
		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
		require.Equal(t, protocol.Role_CLIENT, p.role)
	})

	t.Run("auth exchange: valid credentials are received from a server", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		auth := &mockServerAuthenticator{}
		auth.On("AuthenticateFromMessage", mock.Anything, mock.Anything).Return(true, nil)
		config := makeTestConfigWithWebRtc(t, auth, webRtc)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:      t,
			config: config,
			firstMessageRecv: &protocol.AuthMessage{
				Type: protocol.MessageType_AUTH,
				Role: protocol.Role_COMMUNICATION_SERVER,
			},
		})

		state := makeTestState(test.t, config)
		defer closeState(state)

		var reliableOpenHandler func()
		var unreliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(false).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("registerOpenHandler", unreliableDC, mock.Anything).Run(func(args mock.Arguments) {
			unreliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("detach", reliableDC).Return(test.ReliableRWC, nil).
			On("detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		p, err := initPeer(state, 1, protocol.Role_UNKNOWN_ROLE)
		require.NoError(t, err)

		go unreliableOpenHandler()
		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
		require.Equal(t, protocol.Role_COMMUNICATION_SERVER, p.role)
	})

	t.Run("auth exchange: connecting to known server", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		auth := &mockServerAuthenticator{}
		auth.On("GenerateServerAuthMessage").Return(&protocol.AuthMessage{}, nil).Once()
		config := makeTestConfigWithWebRtc(t, auth, webRtc)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:      t,
			config: config,
			firstMessageRecv: &protocol.TopicMessage{
				Type: protocol.MessageType_TOPIC,
			},
		})

		state := makeTestState(test.t, config)

		var reliableOpenHandler func()
		var unreliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("newConnection", uint64(100000)).Return(conn, nil).
			On("isNew", conn).Return(false).
			On("createReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("registerOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("registerOpenHandler", unreliableDC, mock.Anything).Run(func(args mock.Arguments) {
			unreliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("detach", reliableDC).Return(test.ReliableRWC, nil).
			On("detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("close", conn).Return(nil).Once().
			On("close", conn).Return(errors.New("already closed"))

		_, err := initPeer(state, 100000, protocol.Role_CLIENT)
		require.NoError(t, err)

		go unreliableOpenHandler()
		reliableOpenHandler()

		<-state.unregisterQueue
		auth.AssertExpectations(t)
	})
}

func TestReadReliablePump(t *testing.T) {
	setupPeer := func(t *testing.T, alias uint64, msg proto.Message) *peer {
		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		services := makeTestServices(t)
		p := makeClient(alias, services)
		p.messagesQueue = make(chan *peerMessage, 255)
		p.topicQueue = make(chan topicChange, 255)
		p.unregisterQueue = make(chan *peer, 255)
		reliableDC := mockReadWriteCloser{}
		p.ReliableDC = &reliableDC

		reliableDC.
			On("Read", mock.Anything).Run(func(args mock.Arguments) {
			arg := args.Get(0).([]byte)
			copy(arg, encodedMsg)
		}).Return(len(encodedMsg), nil).Once().
			On("Read", mock.Anything).Return(0, errors.New("stop")).Once()

		return p
	}

	t.Run("topic subscription message", func(t *testing.T) {
		msg := &protocol.TopicSubscriptionMessage{
			Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
			Format: protocol.Format_PLAIN,
			Topics: []byte("topic1 topic2"),
		}

		p := setupPeer(t, 1, msg)
		p.readReliablePump()

		require.Len(t, p.topicQueue, 1)
		change := <-p.topicQueue
		require.Equal(t, uint64(1), change.peer.Alias)
		require.Equal(t, change.rawTopics, msg.Topics)
	})

	t.Run("topic message (client)", func(t *testing.T) {
		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, 1, msg)
		p.readReliablePump()

		require.Len(t, p.messagesQueue, 1)
		peerMessage := <-p.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
	})

	t.Run("topic message (server)", func(t *testing.T) {
		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, 1, msg)
		p.role = protocol.Role_COMMUNICATION_SERVER
		p.readReliablePump()

		require.Len(t, p.messagesQueue, 1)
		peerMessage := <-p.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
	})
}

func TestReadUnreliablePump(t *testing.T) {
	setup := func(t *testing.T, alias uint64, msg proto.Message, webRtc *mockWebRtc) *peer {
		services := makeTestServicesWithWebRtc(t, webRtc)
		p := makeClient(alias, services)
		p.messagesQueue = make(chan *peerMessage, 255)
		p.topicQueue = make(chan topicChange, 255)
		p.unregisterQueue = make(chan *peer, 255)

		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		unreliableDC := mockReadWriteCloser{}
		p.UnreliableDC = &unreliableDC
		unreliableDC.
			On("Read", mock.Anything).Run(func(args mock.Arguments) {
			arg := args.Get(0).([]byte)
			copy(arg, encodedMsg)
		}).Return(len(encodedMsg), nil).Once().
			On("Read", mock.Anything).Return(0, errors.New("stop")).Once()
		return p
	}

	t.Run("sending data before authentication", func(t *testing.T) {
		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		webRtc := makeDefaultMockWebRtc()

		p := setup(t, 1, msg, webRtc)
		p.readUnreliablePump()

		require.Len(t, p.topicQueue, 0)
		webRtc.AssertExpectations(t)
	})

	t.Run("topic message", func(t *testing.T) {
		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		webRtc := makeDefaultMockWebRtc()

		p := setup(t, 1, msg, webRtc)
		p.readUnreliablePump()

		require.Len(t, p.messagesQueue, 1)
		peerMessage := <-p.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
		webRtc.AssertExpectations(t)
	})
}

func TestProcessConnect(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		conn1 := &pion.PeerConnection{}
		conn2 := &pion.PeerConnection{}
		dc := &pion.DataChannel{}

		webRtc := &mockWebRtc{}
		webRtc.
			On("newConnection", uint64(1)).Return(conn1, nil).Once().
			On("isNew", conn1).Return(true).Once().
			On("close", conn1).Return(nil).Once().
			On("createOffer", conn1).Return("offer", nil).Once().
			On("newConnection", uint64(2)).Return(conn2, nil).Once().
			On("isNew", conn2).Return(true).Once().
			On("close", conn2).Return(nil).Once().
			On("createOffer", conn2).Return("offer", nil).Once().
			On("createReliableDataChannel", mock.Anything).Return(dc, nil).
			On("createUnreliableDataChannel", mock.Anything).Return(dc, nil).
			On("registerOpenHandler", mock.Anything, mock.Anything)
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)

		state.connectQueue <- 1
		state.connectQueue <- 2
		state.softStop = true

		Process(state)

		// NOTE: eventually connections will be closed because establish timeout
		<-state.unregisterQueue
		<-state.unregisterQueue

		require.Len(t, state.coordinator.send, 2)
		webRtc.AssertExpectations(t)
	})

	t.Run("create offer error", func(t *testing.T) {
		conn := &pion.PeerConnection{}
		dc := &pion.DataChannel{}

		webRtc := &mockWebRtc{}
		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(true).Once().
			On("close", conn).Return(nil).Once().
			On("createOffer", conn).Return("", errors.New("cannot create offer")).Once().
			On("createReliableDataChannel", conn).Return(dc, nil).
			On("createUnreliableDataChannel", conn).Return(dc, nil).
			On("registerOpenHandler", mock.Anything, mock.Anything)
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)
		state.connectQueue <- 1
		state.softStop = true

		Process(state)

		// NOTE: eventually connection will be closed because establish timeout
		<-state.unregisterQueue

		require.Len(t, state.coordinator.send, 0)
		webRtc.AssertExpectations(t)
	})
}

func TestProcessSubscriptionChange(t *testing.T) {
	setupPeer := func(state *State, alias uint64, role protocol.Role) *peer {
		p := &peer{
			services: state.services,
			Alias:    alias,
			role:     role,
			Topics:   make(map[string]struct{}),
		}

		return addPeer(state, p)
	}

	t.Run("add topic from clients", func(t *testing.T) {
		config := makeTestConfig(t)
		state := makeTestState(t, config)
		defer closeState(state)

		c1 := setupPeer(state, 1, protocol.Role_CLIENT)
		c2 := setupPeer(state, 2, protocol.Role_CLIENT)

		s1 := setupPeer(state, 3, protocol.Role_COMMUNICATION_SERVER)
		s1ReliableDC := mockReadWriteCloser{}
		s1.ReliableDC = &s1ReliableDC
		s1ReliableDC.On("Write", mock.Anything).Return(0, nil)

		state.topicQueue <- topicChange{
			peer:      c1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.topicQueue <- topicChange{
			peer:      c2,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.softStop = true

		Process(state)

		require.Len(t, state.subscriptions, 1)
		require.Contains(t, state.subscriptions, "topic1")
		require.Len(t, state.subscriptions["topic1"].clients, 2)
		require.Len(t, state.subscriptions["topic1"].servers, 0)
		require.Contains(t, state.subscriptions["topic1"].clients, c1)
		require.Contains(t, state.subscriptions["topic1"].clients, c2)
		s1ReliableDC.AssertExpectations(t)

		require.Len(t, c1.Topics, 1)
		require.Contains(t, c1.Topics, "topic1")
		require.Len(t, c2.Topics, 1)
		require.Contains(t, c2.Topics, "topic1")
	})

	t.Run("server to server, but second server is not subscribed", func(t *testing.T) {
		config := makeTestConfig(t)
		state := makeTestState(t, config)
		defer closeState(state)

		s1 := setupPeer(state, 1, protocol.Role_COMMUNICATION_SERVER)
		s2 := setupPeer(state, 2, protocol.Role_COMMUNICATION_SERVER)
		s2ReliableDC := mockReadWriteCloser{}
		s2.ReliableDC = &s2ReliableDC

		state.topicQueue <- topicChange{
			peer:      s1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.softStop = true

		Process(state)

		require.Len(t, state.subscriptions["topic1"].clients, 0)
		require.Len(t, state.subscriptions["topic1"].servers, 1)
		require.Contains(t, state.subscriptions["topic1"].servers, s1)

		s2ReliableDC.AssertExpectations(t)

		require.Len(t, s1.Topics, 1)
		require.Contains(t, s1.Topics, "topic1")
	})

	t.Run("remove topic from clients", func(t *testing.T) {
		config := makeTestConfig(t)
		state := makeTestState(t, config)
		defer closeState(state)

		c1 := setupPeer(state, 1, protocol.Role_CLIENT)
		c1.Topics["topic1"] = struct{}{}
		c2 := setupPeer(state, 2, protocol.Role_CLIENT)
		c2.Topics["topic1"] = struct{}{}

		s1 := setupPeer(state, 3, protocol.Role_COMMUNICATION_SERVER)
		s1ReliableDC := mockReadWriteCloser{}
		s1.ReliableDC = &s1ReliableDC
		s1ReliableDC.On("Write", mock.Anything).Return(0, nil)

		state.subscriptions.AddClientSubscription("topic1", c1)
		state.subscriptions.AddClientSubscription("topic1", c2)

		state.topicQueue <- topicChange{
			peer:      c1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte(""),
		}

		state.topicQueue <- topicChange{
			peer:      c2,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte(""),
		}

		state.softStop = true

		Process(state)

		require.Len(t, state.subscriptions, 0)
		require.Len(t, c1.Topics, 0)
		require.Len(t, c2.Topics, 0)

		s1ReliableDC.AssertExpectations(t)
	})
}

func TestUnregister(t *testing.T) {
	config := makeTestConfig(t)
	state := makeTestState(t, config)
	defer closeState(state)

	p := addPeer(state, makeClient(1, state.services))
	p.Topics["topic1"] = struct{}{}

	p2 := addPeer(state, makeClient(2, state.services))
	p2.Topics["topic1"] = struct{}{}

	state.subscriptions.AddClientSubscription("topic1", p)
	state.subscriptions.AddClientSubscription("topic1", p2)

	state.unregisterQueue <- p
	state.unregisterQueue <- p2
	state.softStop = true

	Process(state)

	require.Len(t, state.peers, 0)
	require.Len(t, state.subscriptions, 0)
}

func TestProcessWebRtcMessage(t *testing.T) {
	t.Run("webrtc offer (on a new peer)", func(t *testing.T) {
		conn := &pion.PeerConnection{}
		dc := &pion.DataChannel{}

		webRtc := &mockWebRtc{}
		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).Once().
			On("isNew", conn).Return(true).Once().
			On("close", conn).Return(nil).Once().
			On("onOffer", conn, "sdp-offer").Return("sdp-answer", nil).Once().
			On("createReliableDataChannel", conn).Return(dc, nil).Once().
			On("createUnreliableDataChannel", conn).Return(dc, nil).Once().
			On("registerOpenHandler", mock.Anything, mock.Anything)
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: 1,
		}

		state.softStop = true

		Process(state)

		// NOTE: eventually connection will be closed because establish timeout
		<-state.unregisterQueue

		require.Len(t, state.coordinator.send, 1)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc offer", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		webRtc.
			On("onOffer", mock.Anything, "sdp-offer").Return("sdp-answer", nil).Twice()
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)

		p := addPeer(state, makeClient(1, state.services))
		p2 := addPeer(state, makeClient(2, state.services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: p.Alias,
		}

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: p2.Alias,
		}

		state.softStop = true

		Process(state)

		require.Len(t, state.coordinator.send, 2)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc offer (offer error)", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		webRtc.
			On("onOffer", mock.Anything, "sdp-offer").Return("sdp-answer", errors.New("offer error")).Once()
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)
		p := addPeer(state, makeClient(1, state.services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(state)

		require.Len(t, state.coordinator.send, 0)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc answer", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		webRtc.
			On("onAnswer", mock.Anything, "sdp-answer").Return(nil).Once()
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)
		p := addPeer(state, makeClient(1, state.services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ANSWER,
			Sdp:       "sdp-answer",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(state)

		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc ice candidate", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		webRtc.
			On("onIceCandidate", mock.Anything, "sdp-candidate").Return(nil).Once()
		config := makeTestConfigWithWebRtc(t, nil, webRtc)

		state := makeTestState(t, config)
		defer closeState(state)

		p := addPeer(state, makeClient(1, state.services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ICE_CANDIDATE,
			Sdp:       "sdp-candidate",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(state)

		webRtc.AssertExpectations(t)
	})
}

func TestProcessTopicMessage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		p1 := &peer{
			Alias: 1,
			conn:  &pion.PeerConnection{},
			Topics: map[string]struct{}{
				"topic1": struct{}{},
			},
		}

		p2ReliableDC := &mockReadWriteCloser{}
		p2ReliableDC.On("Write", mock.Anything).Return(0, nil).Once()
		p2 := &peer{
			Alias:      2,
			ReliableDC: p2ReliableDC,
			conn:       &pion.PeerConnection{},
			Topics: map[string]struct{}{
				"topic1": struct{}{},
			},
		}

		p3 := &peer{
			Alias:  3,
			conn:   &pion.PeerConnection{},
			Topics: make(map[string]struct{}),
		}

		p4 := &peer{
			Alias: 4,
			conn:  &pion.PeerConnection{},
			Topics: map[string]struct{}{
				"topic1": struct{}{},
			},
		}

		webRtc := &mockWebRtc{}
		webRtc.
			On("isClosed", p2.conn).Return(false).Once().
			On("isClosed", p4.conn).Return(true).Once()
		config := makeTestConfigWithWebRtc(t, nil, webRtc)
		state := makeTestState(t, config)
		defer closeState(state)

		p1.services = state.services
		p2.services = state.services
		p3.services = state.services
		p4.services = state.services
		state.peers = append(state.peers, p1, p2, p3, p4)

		state.subscriptions.AddClientSubscription("topic1", p1)
		state.subscriptions.AddClientSubscription("topic1", p2)
		state.subscriptions.AddClientSubscription("topic1", p4)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
		}

		state.softStop = true

		ProcessMessagesQueue(state)

		p2ReliableDC.AssertExpectations(t)
		webRtc.AssertExpectations(t)
	})

	t.Run("success multiple messages", func(t *testing.T) {
		p1 := &peer{
			Alias: 1,
			conn:  &pion.PeerConnection{},
			Topics: map[string]struct{}{
				"topic1": struct{}{},
			},
		}

		p2ReliableDC := &mockReadWriteCloser{}
		p2ReliableDC.On("Write", mock.Anything).Return(0, nil).Twice()
		p2 := &peer{
			Alias:      2,
			ReliableDC: p2ReliableDC,
			conn:       &pion.PeerConnection{},
			Topics: map[string]struct{}{
				"topic1": struct{}{},
			},
		}

		webRtc := &mockWebRtc{}
		webRtc.
			On("isClosed", p2.conn).Return(false).Twice()
		config := makeTestConfigWithWebRtc(t, nil, webRtc)
		state := makeTestState(t, config)
		defer closeState(state)

		p1.services = state.services
		p2.services = state.services
		state.peers = append(state.peers, p1, p2)

		state.subscriptions.AddClientSubscription("topic1", p1)
		state.subscriptions.AddClientSubscription("topic1", p2)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
		}

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
		}

		state.softStop = true

		ProcessMessagesQueue(state)

		p2ReliableDC.AssertExpectations(t)
		webRtc.AssertExpectations(t)
	})
}

func BenchmarkProcessSubscriptionChange(b *testing.B) {
	config := Config{}

	state, err := MakeState(&config)
	require.NoError(b, err)
	defer closeState(state)

	s1 := addPeer(state, makeClient(1, state.services))
	c1 := addPeer(state, makeClient(2, state.services))

	topics1 := []byte("topic1 topic2 topic3 topic4 topic5")
	topics2 := []byte("topic5")
	topics3 := []byte("topic5 topic6 topic7")
	empty := []byte("")

	changes := []topicChange{
		topicChange{
			peer:      s1,
			format:    protocol.Format_PLAIN,
			rawTopics: topics1,
		},
		topicChange{
			peer:      c1,
			format:    protocol.Format_PLAIN,
			rawTopics: topics1,
		},
		topicChange{
			peer:      c1,
			format:    protocol.Format_PLAIN,
			rawTopics: topics2,
		},
		topicChange{
			peer:      c1,
			format:    protocol.Format_PLAIN,
			rawTopics: topics3,
		},
		topicChange{
			peer:      c1,
			format:    protocol.Format_PLAIN,
			rawTopics: empty,
		},
		topicChange{
			peer:      s1,
			format:    protocol.Format_PLAIN,
			rawTopics: empty,
		},
	}

	for i := 0; i < b.N; i++ {
		for _, c := range changes {
			processSubscriptionChange(state, c)
		}
	}
}
