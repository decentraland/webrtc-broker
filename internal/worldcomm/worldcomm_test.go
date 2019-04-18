package worldcomm

import (
	"errors"
	"log"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v2"
	"github.com/stretchr/testify/mock"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	_testing "github.com/decentraland/communications-server-go/internal/testing"
	"github.com/decentraland/communications-server-go/internal/utils"
	"github.com/decentraland/communications-server-go/internal/webrtc"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

var appName = "world-comm-test"

type MockAuthenticator = _testing.MockAuthenticator
type MockReadWriteCloser = _testing.MockReadWriteCloser
type MockWebsocket = _testing.MockWebsocket
type MockWebRtc = _testing.MockWebRtc

func makeMockWebsocket() *MockWebsocket {
	return _testing.MakeMockWebsocket()
}

func makeDefaultMockWebRtc() *MockWebRtc {
	mockWebRtc := &MockWebRtc{}
	mockWebRtc.
		On("Close", mock.Anything).Return(nil).Once().
		On("Close", mock.Anything).Return(errors.New("already closed"))
	return mockWebRtc
}

func makeTestServicesWithWebRtc(t *testing.T, webRtc *MockWebRtc) Services {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	auth := authentication.Make()

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{})
	services := Services{
		Auth:       auth,
		Marshaller: &protocol.Marshaller{},
		Agent:      &WorldCommAgent{Agent: agent},
		WebRtc:     webRtc,
		Log:        logger,
		Zipper:     &utils.GzipCompression{},
	}
	return services
}

func makeTestServices(t *testing.T) Services {
	return makeTestServicesWithWebRtc(t, makeDefaultMockWebRtc())
}

func makeTestState(t *testing.T, services Services) WorldCommunicationState {
	config := Config{
		AuthMethod:              "testAuth",
		Services:                services,
		EstablishSessionTimeout: 1 * time.Second,
	}

	state := MakeState(config)
	return state
}

func makeClient(alias uint64, services Services) *peer {
	return &peer{
		services: services,
		Alias:    alias,
		conn:     &pion.PeerConnection{},
		Topics:   make(map[string]struct{}),
	}
}

func makeServer(alias uint64, services Services) *peer {
	p := makeClient(alias, services)
	p.isServer = true
	return p
}

func addPeer(state *WorldCommunicationState, p *peer) *peer {
	state.Peers = append(state.Peers, p)
	return p
}

func TestCoordinatorSend(t *testing.T) {
	services := makeTestServices(t)
	state := makeTestState(t, services)
	c := makeCoordinator("")
	defer c.Close()

	msg1 := &protocol.PingMessage{}
	encoded1, err := proto.Marshal(msg1)
	require.NoError(t, err)
	require.NoError(t, c.Send(&state, msg1))
	require.Len(t, c.send, 1)

	msg2 := &protocol.PingMessage{}
	encoded2, err := proto.Marshal(msg2)
	require.NoError(t, err)
	require.NoError(t, c.Send(&state, msg2))
	require.Len(t, c.send, 2)

	require.Equal(t, <-c.send, encoded2)
	require.Equal(t, <-c.send, encoded1)
}

func TestCoordinatorReadPump(t *testing.T) {
	setup := func() (WorldCommunicationState, *MockWebsocket) {
		services := makeTestServices(t)
		state := makeTestState(t, services)
		services.Auth.AddOrUpdateAuthenticator("testAuth", &MockAuthenticator{
			GenerateAuthMessage_: func(role protocol.Role) (*protocol.AuthMessage, error) {
				require.Equal(t, role, protocol.Role_COMMUNICATION_SERVER)
				return &protocol.AuthMessage{}, nil
			},
		})

		conn := makeMockWebsocket()
		state.coordinator.conn = conn
		return state, conn
	}

	t.Run("welcome server message", func(t *testing.T) {
		state, conn := setup()
		msg := &protocol.WelcomeMessage{
			Type:             protocol.MessageType_WELCOME,
			Alias:            3,
			AvailableServers: []uint64{1, 2},
		}
		require.NoError(t, conn.PrepareToRead(msg))

		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go state.coordinator.readPump(&state, welcomeChannel)

		welcomeMessage := <-welcomeChannel
		<-state.stop

		require.Equal(t, uint64(3), welcomeMessage.Alias)
	})

	t.Run("webrtc message", func(t *testing.T) {
		state, conn := setup()
		msg := &protocol.WebRtcMessage{
			Type: protocol.MessageType_WEBRTC_ANSWER,
		}
		require.NoError(t, conn.PrepareToRead(msg))
		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go state.coordinator.readPump(&state, welcomeChannel)

		<-state.stop
		require.Len(t, state.webRtcControlQueue, 1)
	})

	t.Run("connect message", func(t *testing.T) {
		state, conn := setup()
		msg := &protocol.ConnectMessage{
			Type:      protocol.MessageType_CONNECT,
			FromAlias: 2,
		}
		require.NoError(t, conn.PrepareToRead(msg))
		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go state.coordinator.readPump(&state, welcomeChannel)

		<-state.stop
		require.Len(t, state.connectQueue, 1)
		require.Equal(t, uint64(2), <-state.connectQueue)
	})
}

func TestCoordinatorWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.PingMessage{})
	require.NoError(t, err)

	services := makeTestServices(t)
	state := makeTestState(t, services)
	i := 0
	conn := makeMockWebsocket()
	conn.OnWrite = func(bytes []byte) {
		require.Equal(t, bytes, msg)
		if i == 1 {
			state.coordinator.Close()
			return
		}
		i += 1
	}
	state.coordinator.conn = conn

	state.coordinator.send <- msg
	state.coordinator.send <- msg

	state.coordinator.writePump(&state)
	require.Equal(t, i, 1)
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
	IsRecvAuthValid  bool
	services         *Services
	firstMessageRecv protocol.Message
}

type authExchangeTest struct {
	t                    *testing.T
	authMessageGenerated bool
	state                *WorldCommunicationState
	WebRtc               *_testing.MockWebRtc
	ReliableDC           *webrtc.DataChannel
	UnreliableDC         *webrtc.DataChannel
	ReliableRWC          *MockReadWriteCloser
	UnreliableRWC        *MockReadWriteCloser
}

func setupAuthExchangeTest(config authExchangeTestConfig) *authExchangeTest {
	test := &authExchangeTest{t: config.t}

	encodedMsg, err := proto.Marshal(config.firstMessageRecv)
	require.NoError(config.t, err)

	authenticator := &MockAuthenticator{
		Authenticate_: func(role protocol.Role, bytes []byte) (bool, error) {
			return config.IsRecvAuthValid, nil
		},
		GenerateAuthMessage_: func(protocol.Role) (*protocol.AuthMessage, error) {
			test.authMessageGenerated = true
			return &protocol.AuthMessage{}, nil
		},
	}

	webRtc := &MockWebRtc{}
	test.ReliableRWC = &MockReadWriteCloser{}
	test.ReliableRWC.
		On("Write", mock.Anything).Return(0, nil).
		On("Read", mock.Anything).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]byte)
		copy(arg, encodedMsg)
	}).Return(len(encodedMsg), nil).Once().
		On("Read", mock.Anything).Return(0, errors.New("stop"))

	test.UnreliableRWC = &MockReadWriteCloser{}
	test.UnreliableRWC.
		On("Write", mock.Anything).Return(0, nil).
		On("Read", mock.Anything).Run(func(args mock.Arguments) {
		arg := args.Get(0).([]byte)
		copy(arg, encodedMsg)
	}).Return(len(encodedMsg), nil).Once().
		On("Read", mock.Anything).Return(0, errors.New("stop"))

	config.services.Auth.AddOrUpdateAuthenticator("testAuth", authenticator)
	config.services.WebRtc = webRtc
	test.WebRtc = webRtc

	return test
}

func TestInitPeer(t *testing.T) {
	t.Run("if no connection is establish eventually the peer is unregistered", func(t *testing.T) {
		services := makeTestServices(t)
		webRtc := &MockWebRtc{}
		services.WebRtc = webRtc

		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		webRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(true).
			On("CreateReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("RegisterOpenHandler", mock.Anything, mock.Anything).
			On("Close", conn).Return(nil).Once().
			On("Close", conn).Return(errors.New("already closed"))

		state := makeTestState(t, services)
		defer closeState(&state)

		_, err := initPeer(&state, 1)
		require.NoError(t, err)

		p := <-state.unregisterQueue
		require.Equal(t, uint64(1), p.Alias)
	})

	t.Run("auth exchange: first message is not auth", func(t *testing.T) {
		services := makeTestServices(t)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:               t,
			IsRecvAuthValid: true,
			services:        &services,
			firstMessageRecv: &protocol.TopicMessage{
				Type: protocol.MessageType_TOPIC,
			},
		})

		state := makeTestState(test.t, services)
		defer closeState(&state)

		var reliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		test.WebRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(false).
			On("CreateReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("RegisterOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("RegisterOpenHandler", unreliableDC, mock.Anything).Once().
			On("Detach", reliableDC).Return(test.ReliableRWC, nil).
			On("Detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("Close", conn).Return(nil).Once().
			On("Close", conn).Return(errors.New("already closed"))

		_, err := initPeer(&state, 1)
		require.NoError(t, err)

		reliableOpenHandler()

		<-state.unregisterQueue
		require.True(t, test.authMessageGenerated)
	})

	t.Run("auth exchange: invalid role received in auth message", func(t *testing.T) {
		services := makeTestServices(t)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:               t,
			IsRecvAuthValid: false,
			services:        &services,
			firstMessageRecv: &protocol.AuthMessage{
				Type:   protocol.MessageType_AUTH,
				Method: "testAuth",
				Role:   protocol.Role_UNKNOWN_ROLE,
			},
		})

		state := makeTestState(test.t, services)
		defer closeState(&state)

		var reliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		test.WebRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(false).
			On("CreateReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("RegisterOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("RegisterOpenHandler", unreliableDC, mock.Anything).Once().
			On("Detach", reliableDC).Return(test.ReliableRWC, nil).
			On("Detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("Close", conn).Return(nil).Once().
			On("Close", conn).Return(errors.New("already closed"))

		_, err := initPeer(&state, 1)
		require.NoError(t, err)

		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
		require.True(t, test.authMessageGenerated)
	})

	t.Run("auth exchange: invalid credentials received", func(t *testing.T) {
		services := makeTestServices(t)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:               t,
			IsRecvAuthValid: false,
			services:        &services,
			firstMessageRecv: &protocol.AuthMessage{
				Type:   protocol.MessageType_AUTH,
				Method: "testAuth",
				Role:   protocol.Role_CLIENT,
			},
		})

		state := makeTestState(test.t, services)
		defer closeState(&state)

		var reliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		test.WebRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(false).
			On("CreateReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("RegisterOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("RegisterOpenHandler", unreliableDC, mock.Anything).Once().
			On("Detach", reliableDC).Return(test.ReliableRWC, nil).
			On("Detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("Close", conn).Return(nil).Once().
			On("Close", conn).Return(errors.New("already closed"))

		_, err := initPeer(&state, 1)
		require.NoError(t, err)

		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
		require.True(t, test.authMessageGenerated)
	})

	t.Run("auth exchange: valid credentials are received from a client", func(t *testing.T) {
		services := makeTestServices(t)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:               t,
			IsRecvAuthValid: true,
			services:        &services,
			firstMessageRecv: &protocol.AuthMessage{
				Type:   protocol.MessageType_AUTH,
				Method: "testAuth",
				Role:   protocol.Role_CLIENT,
			},
		})

		state := makeTestState(test.t, services)
		defer closeState(&state)

		var reliableOpenHandler func()
		var unreliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		test.WebRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(false).
			On("CreateReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("RegisterOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("RegisterOpenHandler", unreliableDC, mock.Anything).Run(func(args mock.Arguments) {
			unreliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("Detach", reliableDC).Return(test.ReliableRWC, nil).
			On("Detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("Close", conn).Return(nil).Once().
			On("Close", conn).Return(errors.New("already closed"))

		p, err := initPeer(&state, 1)
		require.NoError(t, err)

		go unreliableOpenHandler()
		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
		require.False(t, p.isServer)
	})

	t.Run("auth exchange: valid credentials are received from a server", func(t *testing.T) {
		services := makeTestServices(t)
		test := setupAuthExchangeTest(authExchangeTestConfig{
			t:               t,
			IsRecvAuthValid: true,
			services:        &services,
			firstMessageRecv: &protocol.AuthMessage{
				Type:   protocol.MessageType_AUTH,
				Method: "testAuth",
				Role:   protocol.Role_COMMUNICATION_SERVER,
			},
		})

		state := makeTestState(test.t, services)
		defer closeState(&state)

		var reliableOpenHandler func()
		var unreliableOpenHandler func()
		conn := &pion.PeerConnection{}
		reliableDC := &pion.DataChannel{}
		unreliableDC := &pion.DataChannel{}

		test.WebRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(false).
			On("CreateReliableDataChannel", mock.Anything).Return(reliableDC, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(unreliableDC, nil).
			On("RegisterOpenHandler", reliableDC, mock.Anything).Run(func(args mock.Arguments) {
			reliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("RegisterOpenHandler", unreliableDC, mock.Anything).Run(func(args mock.Arguments) {
			unreliableOpenHandler = args.Get(1).(func())
		}).Once().
			On("Detach", reliableDC).Return(test.ReliableRWC, nil).
			On("Detach", unreliableDC).Return(test.UnreliableRWC, nil).
			On("Close", conn).Return(nil).Once().
			On("Close", conn).Return(errors.New("already closed"))

		p, err := initPeer(&state, 1)
		require.NoError(t, err)

		go unreliableOpenHandler()
		reliableOpenHandler()

		// NOTE: called by peer.Close() on read error
		<-state.unregisterQueue
		require.True(t, p.isServer)
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
		reliableDC := MockReadWriteCloser{}
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
		p.isServer = true
		p.readReliablePump()

		require.Len(t, p.messagesQueue, 1)
		peerMessage := <-p.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
	})
}

func TestReadUnreliablePump(t *testing.T) {
	setup := func(t *testing.T, alias uint64, msg proto.Message, webRtc *MockWebRtc) *peer {
		services := makeTestServicesWithWebRtc(t, webRtc)
		p := makeClient(alias, services)
		p.messagesQueue = make(chan *peerMessage, 255)
		p.topicQueue = make(chan topicChange, 255)
		p.unregisterQueue = make(chan *peer, 255)

		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		unreliableDC := MockReadWriteCloser{}
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

		webRtc := &MockWebRtc{}
		webRtc.
			On("NewConnection", uint64(1)).Return(conn1, nil).Once().
			On("IsNew", conn1).Return(true).Once().
			On("Close", conn1).Return(nil).Once().
			On("CreateOffer", conn1).Return("offer", nil).Once().
			On("NewConnection", uint64(2)).Return(conn2, nil).Once().
			On("IsNew", conn2).Return(true).Once().
			On("Close", conn2).Return(nil).Once().
			On("CreateOffer", conn2).Return("offer", nil).Once().
			On("CreateReliableDataChannel", mock.Anything).Return(dc, nil).
			On("CreateUnreliableDataChannel", mock.Anything).Return(dc, nil).
			On("RegisterOpenHandler", mock.Anything, mock.Anything)
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)

		state.connectQueue <- 1
		state.connectQueue <- 2
		state.softStop = true

		Process(&state)

		// NOTE: eventually connections will be closed because establish timeout
		<-state.unregisterQueue
		<-state.unregisterQueue

		require.Len(t, state.coordinator.send, 2)
		webRtc.AssertExpectations(t)
	})

	t.Run("create offer error", func(t *testing.T) {
		conn := &pion.PeerConnection{}
		dc := &pion.DataChannel{}

		webRtc := &MockWebRtc{}
		webRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).
			On("IsNew", conn).Return(true).Once().
			On("Close", conn).Return(nil).Once().
			On("CreateOffer", conn).Return("", errors.New("cannot create offer")).Once().
			On("CreateReliableDataChannel", conn).Return(dc, nil).
			On("CreateUnreliableDataChannel", conn).Return(dc, nil).
			On("RegisterOpenHandler", mock.Anything, mock.Anything)
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)
		state.connectQueue <- 1
		state.softStop = true

		Process(&state)

		// NOTE: eventually connection will be closed because establish timeout
		<-state.unregisterQueue

		require.Len(t, state.coordinator.send, 0)
		webRtc.AssertExpectations(t)
	})
}

func TestProcessSubscriptionChange(t *testing.T) {
	setupPeer := func(state *WorldCommunicationState, alias uint64, isServer bool) *peer {
		p := &peer{
			services: state.services,
			Alias:    alias,
			isServer: isServer,
			Topics:   make(map[string]struct{}),
		}

		return addPeer(state, p)
	}

	t.Run("add topic from clients", func(t *testing.T) {
		services := makeTestServices(t)
		state := makeTestState(t, services)
		defer closeState(&state)

		c1 := setupPeer(&state, 1, false)
		c2 := setupPeer(&state, 2, false)

		s1 := setupPeer(&state, 3, true)
		s1ReliableDC := MockReadWriteCloser{}
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

		Process(&state)

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
		services := makeTestServices(t)
		state := makeTestState(t, services)
		defer closeState(&state)

		s1 := setupPeer(&state, 1, true)
		s2 := setupPeer(&state, 2, true)
		s2ReliableDC := MockReadWriteCloser{}
		s2.ReliableDC = &s2ReliableDC

		state.topicQueue <- topicChange{
			peer:      s1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.softStop = true

		Process(&state)

		require.Len(t, state.subscriptions["topic1"].clients, 0)
		require.Len(t, state.subscriptions["topic1"].servers, 1)
		require.Contains(t, state.subscriptions["topic1"].servers, s1)

		s2ReliableDC.AssertExpectations(t)

		require.Len(t, s1.Topics, 1)
		require.Contains(t, s1.Topics, "topic1")
	})

	t.Run("remove topic from clients", func(t *testing.T) {
		services := makeTestServices(t)
		state := makeTestState(t, services)
		defer closeState(&state)

		c1 := setupPeer(&state, 1, false)
		c1.Topics["topic1"] = struct{}{}
		c2 := setupPeer(&state, 2, false)
		c2.Topics["topic1"] = struct{}{}

		s1 := setupPeer(&state, 3, true)
		s1ReliableDC := MockReadWriteCloser{}
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

		Process(&state)

		require.Len(t, state.subscriptions, 0)
		require.Len(t, c1.Topics, 0)
		require.Len(t, c2.Topics, 0)

		s1ReliableDC.AssertExpectations(t)
	})
}

func TestUnregister(t *testing.T) {
	services := makeTestServices(t)
	state := makeTestState(t, services)
	defer closeState(&state)

	p := addPeer(&state, makeClient(1, services))
	p.Topics["topic1"] = struct{}{}

	p2 := addPeer(&state, makeClient(2, services))
	p2.Topics["topic1"] = struct{}{}

	state.subscriptions.AddClientSubscription("topic1", p)
	state.subscriptions.AddClientSubscription("topic1", p2)

	state.unregisterQueue <- p
	state.unregisterQueue <- p2
	state.softStop = true

	Process(&state)

	require.Len(t, state.Peers, 0)
	require.Len(t, state.subscriptions, 0)
}

func TestProcessWebRtcMessage(t *testing.T) {
	t.Run("webrtc offer (on a new peer)", func(t *testing.T) {
		conn := &pion.PeerConnection{}
		dc := &pion.DataChannel{}

		webRtc := &MockWebRtc{}
		webRtc.
			On("NewConnection", uint64(1)).Return(conn, nil).Once().
			On("IsNew", conn).Return(true).Once().
			On("Close", conn).Return(nil).Once().
			On("OnOffer", conn, "sdp-offer").Return("sdp-answer", nil).Once().
			On("CreateReliableDataChannel", conn).Return(dc, nil).Once().
			On("CreateUnreliableDataChannel", conn).Return(dc, nil).Once().
			On("RegisterOpenHandler", mock.Anything, mock.Anything)
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: 1,
		}

		state.softStop = true

		Process(&state)

		// NOTE: eventually connection will be closed because establish timeout
		<-state.unregisterQueue

		require.Len(t, state.coordinator.send, 1)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc offer", func(t *testing.T) {
		webRtc := &MockWebRtc{}
		webRtc.
			On("OnOffer", mock.Anything, "sdp-offer").Return("sdp-answer", nil).Twice()
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)

		p := addPeer(&state, makeClient(1, services))
		p2 := addPeer(&state, makeClient(2, services))

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

		Process(&state)

		require.Len(t, state.coordinator.send, 2)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc offer (offer error)", func(t *testing.T) {
		webRtc := &MockWebRtc{}
		webRtc.
			On("OnOffer", mock.Anything, "sdp-offer").Return("sdp-answer", errors.New("offer error")).Once()
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)
		p := addPeer(&state, makeClient(1, services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(&state)

		require.Len(t, state.coordinator.send, 0)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc answer", func(t *testing.T) {
		webRtc := &MockWebRtc{}
		webRtc.
			On("OnAnswer", mock.Anything, "sdp-answer").Return(nil).Once()
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)
		p := addPeer(&state, makeClient(1, services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ANSWER,
			Sdp:       "sdp-answer",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(&state)

		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc ice candidate", func(t *testing.T) {
		webRtc := &MockWebRtc{}
		webRtc.
			On("OnIceCandidate", mock.Anything, "sdp-candidate").Return(nil).Once()
		services := makeTestServicesWithWebRtc(t, webRtc)

		state := makeTestState(t, services)
		defer closeState(&state)

		p := addPeer(&state, makeClient(1, services))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ICE_CANDIDATE,
			Sdp:       "sdp-candidate",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(&state)

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

		p2ReliableDC := &MockReadWriteCloser{}
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

		webRtc := &MockWebRtc{}
		webRtc.
			On("IsClosed", p2.conn).Return(false).Once().
			On("IsClosed", p4.conn).Return(true).Once()
		services := makeTestServicesWithWebRtc(t, webRtc)
		state := makeTestState(t, services)
		defer closeState(&state)

		p1.services = services
		p2.services = services
		p3.services = services
		p4.services = services
		log.Println("")
		state.Peers = append(state.Peers, p1, p2, p3, p4)

		state.subscriptions.AddClientSubscription("topic1", p1)
		state.subscriptions.AddClientSubscription("topic1", p2)
		state.subscriptions.AddClientSubscription("topic1", p4)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
		}

		state.softStop = true

		ProcessMessagesQueue(&state)

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

		p2ReliableDC := &MockReadWriteCloser{}
		p2ReliableDC.On("Write", mock.Anything).Return(0, nil).Twice()
		p2 := &peer{
			Alias:      2,
			ReliableDC: p2ReliableDC,
			conn:       &pion.PeerConnection{},
			Topics: map[string]struct{}{
				"topic1": struct{}{},
			},
		}

		webRtc := &MockWebRtc{}
		webRtc.
			On("IsClosed", p2.conn).Return(false).Twice()
		services := makeTestServicesWithWebRtc(t, webRtc)
		state := makeTestState(t, services)
		defer closeState(&state)

		p1.services = services
		p2.services = services
		state.Peers = append(state.Peers, p1, p2)

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

		ProcessMessagesQueue(&state)

		p2ReliableDC.AssertExpectations(t)
		webRtc.AssertExpectations(t)
	})
}

func BenchmarkProcessSubscriptionChange(b *testing.B) {
	agent, err := agent.Make(appName, "")
	require.NoError(b, err)

	auth := authentication.Make()
	services := Services{
		Auth:       auth,
		Marshaller: &protocol.Marshaller{},
		Agent:      &WorldCommAgent{Agent: agent},
		WebRtc:     nil,
		Log:        logrus.New(),
		Zipper:     &utils.GzipCompression{},
	}

	config := Config{
		AuthMethod: "testAuth",
		Services:   services,
	}

	state := MakeState(config)
	defer closeState(&state)

	s1 := addPeer(&state, makeClient(1, services))
	c1 := addPeer(&state, makeClient(2, services))

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
			ProcessSubscriptionChange(&state, c)
		}
	}
}
