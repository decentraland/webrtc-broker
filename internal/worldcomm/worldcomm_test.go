package worldcomm

import (
	"errors"
	"strings"
	"testing"

	"github.com/decentraland/communications-server-go/internal/agent"
	_testing "github.com/decentraland/communications-server-go/internal/testing"
	"github.com/decentraland/communications-server-go/internal/webrtc"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

var appName = "world-comm-test"

type MockWebRtcConnection = _testing.MockWebRtcConnection
type MockWebRtc = _testing.MockWebRtc
type MockAuthenticator = _testing.MockAuthenticator

func makeMockWebRtcConnection() *MockWebRtcConnection {
	return _testing.MakeMockWebRtcConnection()
}

func setupSimpleAuthenticator(state *WorldCommunicationState, name string, isValid bool) *MockAuthenticator {
	auth := _testing.MakeWithAuthResponse(isValid)
	state.Auth.AddOrUpdateAuthenticator(name, auth)
	return auth
}

func makeTestState(t *testing.T) WorldCommunicationState {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)

	state := MakeState(agent, "testAuth", "")
	return state
}

func makeClient(alias string, conn *MockWebRtcConnection) *peer {
	return &peer{
		Alias:          alias,
		conn:           conn,
		sendReliable:   make(chan []byte, 256),
		sendUnreliable: make(chan []byte, 256),
		Topics:         make(map[string]struct{}),
	}
}

func makeServer(alias string, conn *MockWebRtcConnection) *peer {
	p := makeClient(alias, conn)
	p.isServer = true
	return p
}

func addPeer(state *WorldCommunicationState, p *peer) *peer {
	state.Peers = append(state.Peers, p)
	return p
}

func TestTopicSubscriptions(t *testing.T) {

	t.Run("add client subscription", func(t *testing.T) {
		c1 := makeClient("peer1", nil)
		c2 := makeClient("peer2", nil)
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
		s1 := makeServer("peer1", nil)
		s2 := makeServer("peer2", nil)
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
		c1 := makeClient("peer1", nil)
		c2 := makeClient("peer2", nil)
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
		s1 := makeServer("peer1", nil)
		s2 := makeServer("peer2", nil)
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
		c1 := makeClient("peer1", nil)
		s1 := makeServer("peer2", nil)
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
		c1 := makeClient("peer1", nil)
		s1 := makeServer("peer2", nil)
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

func TestPeerWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.PingMessage{})
	require.NoError(t, err)

	setup := func(write func(conn *MockWebRtcConnection, rawMsg []byte) error) (*peer, WorldCommunicationState) {
		conn := makeMockWebRtcConnection()

		conn.WriteReliable_ = write
		conn.WriteUnreliable_ = write
		p := makeClient("peer1", conn)
		state := makeTestState(t)

		return p, state
	}

	t.Run("success", func(t *testing.T) {
		i := 0
		write := func(conn *MockWebRtcConnection, rawMsg []byte) error {
			require.Equal(t, rawMsg, msg)
			if i == 3 {
				return errors.New("stop")
			}
			i += 1
			return nil
		}
		p, state := setup(write)
		defer closeState(&state)

		p.sendReliable <- msg
		p.sendReliable <- msg
		p.sendUnreliable <- msg
		p.sendUnreliable <- msg

		p.writePump(&state)
		require.Equal(t, 3, i)
		require.True(t, p.isClosed)
		require.Len(t, p.sendReliable, 0)
		require.Len(t, p.sendUnreliable, 0)
	})

	t.Run("on reliable channel first write error", func(t *testing.T) {
		write := func(conn *MockWebRtcConnection, rawMsg []byte) error {
			return errors.New("stop")
		}
		p, state := setup(write)
		defer closeState(&state)

		p.sendReliable <- msg

		p.writePump(&state)
		require.True(t, p.isClosed)
	})

	t.Run("on reliable channel inner write error", func(t *testing.T) {
		i := 0
		write := func(conn *MockWebRtcConnection, rawMsg []byte) error {
			if i == 1 {
				return errors.New("stop")
			}
			i += 1
			return nil
		}
		p, state := setup(write)
		defer closeState(&state)

		p.sendReliable <- msg
		p.sendReliable <- msg

		p.writePump(&state)
		require.True(t, p.isClosed)
	})

	t.Run("on unreliable channel first write error", func(t *testing.T) {
		write := func(conn *MockWebRtcConnection, rawMsg []byte) error {
			return errors.New("stop")
		}
		p, state := setup(write)
		defer closeState(&state)

		p.sendUnreliable <- msg

		p.writePump(&state)
		require.True(t, p.isClosed)
	})

	t.Run("on unreliable channel inner write error", func(t *testing.T) {
		i := 0
		write := func(conn *MockWebRtcConnection, rawMsg []byte) error {
			if i == 1 {
				return errors.New("stop")
			}
			i += 1
			return nil
		}
		p, state := setup(write)
		defer closeState(&state)

		p.sendUnreliable <- msg
		p.sendUnreliable <- msg

		p.writePump(&state)
		require.True(t, p.isClosed)
	})
}

func TestReadPeerPump(t *testing.T) {
	setupPeer := func(t *testing.T, alias string, msg proto.Message) *peer {
		conn := makeMockWebRtcConnection()
		p := makeClient(alias, conn)
		p.SetIsAuthenticated(true)

		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		i := 0
		read := func(conn *MockWebRtcConnection, rawMsg []byte) (int, error) {
			if i == 0 {
				i += 1
				copy(rawMsg, encodedMsg)
				return len(encodedMsg), nil
			}

			return 0, errors.New("closed")
		}

		conn.ReadReliable_ = read
		conn.ReadUnreliable_ = read
		return p
	}

	t.Run("sending data before authentication (reliable)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.SetIsAuthenticated(false)
		p.readReliablePump(&state)

		require.Len(t, state.topicQueue, 0)
		require.True(t, p.isClosed)
	})

	t.Run("sending data before authentication (unreliable)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.SetIsAuthenticated(false)
		p.readUnreliablePump(&state)

		require.Len(t, state.topicQueue, 0)
		require.True(t, p.isClosed)
	})

	t.Run("authentication (unknown role)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.AuthMessage{
			Type:   protocol.MessageType_AUTH,
			Method: "testAuth",
			Role:   protocol.Role_UNKNOWN_ROLE,
		}

		p := setupPeer(t, "peer1", msg)
		p.SetIsAuthenticated(false)
		p.readReliablePump(&state)

		require.True(t, p.isClosed)
		require.False(t, p.IsAuthenticated())
	})

	t.Run("client authentication accepted", func(t *testing.T) {
		state := makeTestState(t)
		setupSimpleAuthenticator(&state, "testAuth", true)
		defer closeState(&state)

		msg := &protocol.AuthMessage{
			Type:   protocol.MessageType_AUTH,
			Method: "testAuth",
			Role:   protocol.Role_CLIENT,
		}

		p := setupPeer(t, "peer1", msg)
		p.SetIsAuthenticated(false)
		p.readReliablePump(&state)

		require.True(t, p.isClosed)
		require.True(t, p.IsAuthenticated())
		require.False(t, p.isServer)
	})

	t.Run("server authentication accepted", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		auth := setupSimpleAuthenticator(&state, "testAuth", true)

		authMessageGenerated := false
		auth.GenerateAuthMessage_ = func(protocol.Role) (*protocol.AuthMessage, error) {
			authMessageGenerated = true
			return &protocol.AuthMessage{}, nil
		}

		msg := &protocol.AuthMessage{
			Type:   protocol.MessageType_AUTH,
			Method: "testAuth",
			Role:   protocol.Role_COMMUNICATION_SERVER,
		}

		p := setupPeer(t, "peer1", msg)
		p.SetIsAuthenticated(false)
		p.readReliablePump(&state)

		require.True(t, authMessageGenerated)
		require.True(t, p.isClosed)
		require.True(t, p.IsAuthenticated())
		require.True(t, p.isServer)

		require.Len(t, p.sendReliable, 1)
	})

	t.Run("authentication rejected", func(t *testing.T) {
		state := makeTestState(t)
		setupSimpleAuthenticator(&state, "testAuth", false)
		defer closeState(&state)

		msg := &protocol.AuthMessage{
			Type:   protocol.MessageType_AUTH,
			Method: "testAuth",
			Role:   protocol.Role_CLIENT,
		}

		p := setupPeer(t, "peer1", msg)
		p.SetIsAuthenticated(false)
		p.readReliablePump(&state)

		require.True(t, p.isClosed)
		require.False(t, p.IsAuthenticated())
		require.False(t, p.isServer)
	})

	t.Run("topic subscription message", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.TopicSubscriptionMessage{
			Type:   protocol.MessageType_TOPIC_SUBSCRIPTION,
			Format: protocol.Format_PLAIN,
			Topics: []byte("topic1 topic2"),
		}

		p := setupPeer(t, "peer1", msg)
		p.readReliablePump(&state)

		require.Len(t, state.topicQueue, 1)
		change := <-state.topicQueue
		require.Equal(t, "peer1", change.peer.Alias)
		require.Equal(t, change.rawTopics, msg.Topics)
	})

	t.Run("reliable topic message (client)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.readReliablePump(&state)

		require.Len(t, state.messagesQueue, 1)
		peerMessage := <-state.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
	})

	t.Run("reliable topic message (server)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.isServer = true
		p.readReliablePump(&state)

		require.Len(t, state.messagesQueue, 1)
		peerMessage := <-state.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
	})

	t.Run("unreliable topic message", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.readUnreliablePump(&state)

		require.Len(t, state.messagesQueue, 1)
		peerMessage := <-state.messagesQueue
		require.Equal(t, peerMessage.from, p)
		require.Equal(t, "topic1", peerMessage.topic)
	})
}

func TestProcessConnect(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		i := 0
		createOffer := func(conn *MockWebRtcConnection) (string, error) {
			i += 1
			return "offer", nil
		}
		state.webRtc = &MockWebRtc{
			NewConnection_: func(*MockWebRtc) (webrtc.IWebRtcConnection, error) {
				conn := makeMockWebRtcConnection()
				conn.CreateOffer_ = createOffer
				return conn, nil
			},
		}

		state.connectQueue <- "peer1"
		state.connectQueue <- "peer2"
		state.softStop = true

		Process(&state)

		require.Equal(t, 2, i)
		require.Len(t, state.coordinator.send, 2)
	})

	t.Run("create offer error", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		state.webRtc = &MockWebRtc{
			NewConnection_: func(*MockWebRtc) (webrtc.IWebRtcConnection, error) {
				conn := makeMockWebRtcConnection()
				conn.CreateOffer_ = func(conn *MockWebRtcConnection) (string, error) {
					return "", errors.New("cannot create offer")
				}
				return conn, nil
			},
		}

		state.connectQueue <- "peer1"
		state.softStop = true

		Process(&state)

		require.Len(t, state.coordinator.send, 0)
	})
}

func TestProcessTopicChange(t *testing.T) {
	t.Run("add topic from clients", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		p1 := addPeer(&state, makeClient("peer1", nil))
		p2 := addPeer(&state, makeClient("peer2", nil))
		p3 := addPeer(&state, makeServer("peer3", nil))

		state.topicQueue <- topicChange{
			peer:      p1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.topicQueue <- topicChange{
			peer:      p2,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.softStop = true

		Process(&state)

		// NOTE server should subscribe to topic and send the add topic message to
		// the others connected servers
		require.Len(t, state.subscriptions, 1)
		require.Contains(t, state.subscriptions, "topic1")
		require.Len(t, state.subscriptions["topic1"].clients, 2)
		require.Len(t, state.subscriptions["topic1"].servers, 0)
		require.Contains(t, state.subscriptions["topic1"].clients, p1)
		require.Contains(t, state.subscriptions["topic1"].clients, p2)
		require.Len(t, p3.sendReliable, 1)

		require.Len(t, p1.Topics, 1)
		require.Contains(t, p1.Topics, "topic1")
		require.Len(t, p2.Topics, 1)
		require.Contains(t, p2.Topics, "topic1")
	})

	t.Run("add topic from server", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		p1 := addPeer(&state, makeServer("peer1", nil))
		p2 := addPeer(&state, makeServer("peer2", nil))

		state.topicQueue <- topicChange{
			peer:      p1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte("topic1"),
		}
		state.softStop = true

		Process(&state)

		require.Len(t, state.subscriptions["topic1"].clients, 0)
		require.Len(t, state.subscriptions["topic1"].servers, 1)
		require.Contains(t, state.subscriptions["topic1"].servers, p1)

		require.Len(t, p2.sendReliable, 0)

		require.Len(t, p1.Topics, 1)
		require.Contains(t, p1.Topics, "topic1")
	})

	t.Run("remove topic from clients", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		p1 := addPeer(&state, makeClient("peer1", nil))
		p1.Topics["topic1"] = struct{}{}
		p2 := addPeer(&state, makeClient("peer2", nil))
		p2.Topics["topic1"] = struct{}{}
		p3 := addPeer(&state, makeServer("peer3", nil))

		state.subscriptions.AddClientSubscription("topic1", p1)
		state.subscriptions.AddClientSubscription("topic1", p2)

		state.topicQueue <- topicChange{
			peer:      p1,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte(""),
		}

		state.topicQueue <- topicChange{
			peer:      p2,
			format:    protocol.Format_PLAIN,
			rawTopics: []byte(""),
		}

		state.softStop = true

		Process(&state)

		require.Len(t, state.subscriptions, 0)
		require.Len(t, p1.Topics, 0)
		require.Len(t, p2.Topics, 0)

		// NOTE: topics broadcasted
		require.Len(t, p3.sendReliable, 1)
	})
}

func TestUnregister(t *testing.T) {
	state := makeTestState(t)
	defer closeState(&state)

	p := addPeer(&state, makeClient("peer1", nil))
	p.Topics["topic1"] = struct{}{}

	p2 := addPeer(&state, makeClient("peer2", nil))
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
		state := makeTestState(t)
		defer closeState(&state)

		i := 0
		onOffer := func(conn *MockWebRtcConnection, sdp string) (string, error) {
			i += 1
			require.Equal(t, "sdp-offer", sdp)
			return "sdp-answer", nil
		}

		conn := makeMockWebRtcConnection()
		conn.OnOffer_ = onOffer

		state.webRtc = &MockWebRtc{
			NewConnection_: func(*MockWebRtc) (webrtc.IWebRtcConnection, error) {
				return conn, nil
			},
		}

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: "peer1",
		}

		state.softStop = true

		Process(&state)

		require.Equal(t, 1, i)
		require.Len(t, state.coordinator.send, 1)
		require.Len(t, state.Peers, 1)
		require.Equal(t, state.Peers[0].Alias, "peer1")
	})

	t.Run("webrtc offer", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		i := 0
		onOffer := func(conn *MockWebRtcConnection, sdp string) (string, error) {
			i += 1
			require.Equal(t, "sdp-offer", sdp)
			return "sdp-answer", nil
		}

		conn := makeMockWebRtcConnection()
		conn.OnOffer_ = onOffer
		p := addPeer(&state, makeClient("peer1", conn))
		conn2 := makeMockWebRtcConnection()
		conn2.OnOffer_ = onOffer
		p2 := addPeer(&state, makeClient("peer2", conn2))

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

		require.Equal(t, 2, i)
		require.Len(t, state.coordinator.send, 2)
	})

	t.Run("webrtc offer (offer error)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		i := 0
		onOffer := func(conn *MockWebRtcConnection, sdp string) (string, error) {
			i += 1
			require.Equal(t, "sdp-offer", sdp)
			return "", errors.New("offer error")
		}

		conn := makeMockWebRtcConnection()
		conn.OnOffer_ = onOffer
		p := addPeer(&state, makeClient("peer1", conn))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(&state)

		require.Equal(t, 1, i)
		require.Len(t, state.coordinator.send, 0)
	})

	t.Run("webrtc answer", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		i := 0
		conn := makeMockWebRtcConnection()
		conn.OnAnswer_ = func(conn *MockWebRtcConnection, sdp string) error {
			i += 1
			require.Equal(t, "sdp-answer", sdp)
			return nil
		}

		p := addPeer(&state, makeClient("peer1", conn))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ANSWER,
			Sdp:       "sdp-answer",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(&state)

		require.Equal(t, 1, i)
	})

	t.Run("webrtc ice candidate", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		i := 0
		conn := makeMockWebRtcConnection()
		conn.OnIceCandidate_ = func(conn *MockWebRtcConnection, sdp string) error {
			i += 1
			require.Equal(t, "sdp-candidate", sdp)
			return nil
		}

		p := addPeer(&state, makeClient("peer1", conn))

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ICE_CANDIDATE,
			Sdp:       "sdp-candidate",
			FromAlias: p.Alias,
		}

		state.softStop = true

		Process(&state)

		require.Equal(t, 1, i)
	})
}

func TestProcessPeerMessage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		conn1 := makeMockWebRtcConnection()
		p1 := addPeer(&state, makeClient("peer1", conn1))
		p1.Topics["topic1"] = struct{}{}

		conn2 := makeMockWebRtcConnection()
		p2 := addPeer(&state, makeClient("peer2", conn2))
		p2.Topics["topic1"] = struct{}{}

		conn3 := makeMockWebRtcConnection()
		p3 := addPeer(&state, makeClient("peer3", conn3))

		conn4 := makeMockWebRtcConnection()
		p4 := addPeer(&state, makeClient("peer4", conn4))
		p4.Topics["topic1"] = struct{}{}
		p4.isClosed = true

		state.subscriptions.AddClientSubscription("topic1", p1)
		state.subscriptions.AddClientSubscription("topic1", p2)
		state.subscriptions.AddClientSubscription("topic1", p4)

		rawMsg, err := proto.Marshal(&protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		})
		require.NoError(t, err)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
			rawMsg:   rawMsg,
		}

		state.softStop = true

		Process(&state)

		require.Len(t, p1.sendReliable, 0) // NOTE don't send it to itself
		require.Len(t, p2.sendReliable, 1) // NOTE p2 is ready and it's listening to the topic
		require.Len(t, p3.sendReliable, 0) // NOTE p3 is ready but it's not listening to the topic
		require.Len(t, p4.sendReliable, 0) // NOTE p4 is listening to the topic, but is closed
	})

	t.Run("success multiple messages", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		conn1 := makeMockWebRtcConnection()
		p1 := addPeer(&state, makeClient("peer1", conn1))
		p1.Topics["topic1"] = struct{}{}

		conn2 := makeMockWebRtcConnection()
		p2 := addPeer(&state, makeClient("peer2", conn2))
		p2.Topics["topic1"] = struct{}{}

		state.subscriptions.AddClientSubscription("topic1", p1)
		state.subscriptions.AddClientSubscription("topic1", p2)

		rawMsg, err := proto.Marshal(&protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		})
		require.NoError(t, err)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
			rawMsg:   rawMsg,
		}

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     p1,
			rawMsg:   rawMsg,
		}

		state.softStop = true

		Process(&state)

		require.Len(t, p1.sendReliable, 0) // NOTE don't send it to itself
		require.Len(t, p2.sendReliable, 2) // NOTE p2 is ready and it's listening to the topic
	})
}

func TestProcessServerRegistered(t *testing.T) {
	t.Run("no subscriptions", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		s1 := makeServer("peer1", nil)
		s2 := makeServer("peer2", nil)

		state.serverRegisteredQueue <- s1
		state.serverRegisteredQueue <- s2
		state.softStop = true

		Process(&state)

		require.Len(t, s1.sendReliable, 1)
		require.Len(t, s2.sendReliable, 1)

		message := &protocol.TopicSubscriptionMessage{}

		rawMsg := <-s1.sendReliable
		require.NoError(t, proto.Unmarshal(rawMsg, message))
		require.Equal(t, protocol.MessageType_TOPIC_SUBSCRIPTION, message.Type)
		require.Equal(t, protocol.Format_GZIP, message.Format)
		rawTopics, err := state.zipper.Unzip(message.Topics)
		require.NoError(t, err)
		require.Len(t, rawTopics, 0)

		rawMsg = <-s2.sendReliable
		require.NoError(t, proto.Unmarshal(rawMsg, message))
		require.Equal(t, protocol.MessageType_TOPIC_SUBSCRIPTION, message.Type)
		rawTopics, err = state.zipper.Unzip(message.Topics)
		require.NoError(t, err)
		require.Len(t, rawTopics, 0)
	})

	t.Run("with subscriptions", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		c1 := makeClient("peer1", nil)
		s1 := makeServer("peer2", nil)
		s2 := makeServer("peer3", nil)

		state.subscriptions.AddClientSubscription("topic1", c1)
		state.subscriptions.AddClientSubscription("topic2", c1)

		state.serverRegisteredQueue <- s1
		state.serverRegisteredQueue <- s2
		state.softStop = true

		Process(&state)

		require.Len(t, s1.sendReliable, 1)
		require.Len(t, s2.sendReliable, 1)

		message := &protocol.TopicSubscriptionMessage{}

		rawMsg := <-s1.sendReliable
		require.NoError(t, proto.Unmarshal(rawMsg, message))
		require.Equal(t, protocol.MessageType_TOPIC_SUBSCRIPTION, message.Type)
		require.Equal(t, protocol.Format_GZIP, message.Format)
		rawTopics, err := state.zipper.Unzip(message.Topics)
		require.NoError(t, err)
		topics := strings.Split(string(rawTopics), " ")
		require.Len(t, topics, 2)
		require.Contains(t, topics, "topic1")
		require.Contains(t, topics, "topic2")

		rawMsg = <-s2.sendReliable
		require.NoError(t, proto.Unmarshal(rawMsg, message))
		require.Equal(t, protocol.MessageType_TOPIC_SUBSCRIPTION, message.Type)
		rawTopics, err = state.zipper.Unzip(message.Topics)
		require.NoError(t, err)
		topics = strings.Split(string(rawTopics), " ")
		require.Len(t, topics, 2)
		require.Contains(t, topics, "topic1")
		require.Contains(t, topics, "topic2")
	})
}

func BenchmarkProcessTopicChange(b *testing.B) {
	agent, err := agent.Make(appName, "")
	require.NoError(b, err)

	state := MakeState(agent, "testAuth", "")
	defer closeState(&state)

	s1 := addPeer(&state, makeClient("peer1", nil))
	c1 := addPeer(&state, makeClient("peer2", nil))

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
			ProcessTopicChange(&state, c)
		}
	}
}
