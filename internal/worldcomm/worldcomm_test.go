package worldcomm

import (
	"errors"
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
		alias:          alias,
		conn:           conn,
		sendReliable:   make(chan []byte, 256),
		sendUnreliable: make(chan []byte, 256),
		Topics:         make(map[string]bool),
	}
}

func makeServer(alias string, conn *MockWebRtcConnection) *peer {
	p := makeClient(alias, conn)
	p.isServer = true
	return p
}

func addPeer(state *WorldCommunicationState, p *peer) *peer {
	state.Peers[p.alias] = p
	return p
}

func TestPeerWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.PingMessage{})
	require.NoError(t, err)

	setup := func(write func(conn *MockWebRtcConnection, bytes []byte) error) (*peer, WorldCommunicationState) {
		conn := makeMockWebRtcConnection()

		conn.WriteReliable_ = write
		conn.WriteUnreliable_ = write
		p := makeClient("peer1", conn)
		state := makeTestState(t)

		return p, state
	}

	t.Run("success", func(t *testing.T) {
		i := 0
		write := func(conn *MockWebRtcConnection, bytes []byte) error {
			require.Equal(t, bytes, msg)
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
		write := func(conn *MockWebRtcConnection, bytes []byte) error {
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
		write := func(conn *MockWebRtcConnection, bytes []byte) error {
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
		write := func(conn *MockWebRtcConnection, bytes []byte) error {
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
		write := func(conn *MockWebRtcConnection, bytes []byte) error {
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
		p.IsAuthenticated = true

		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		i := 0
		read := func(conn *MockWebRtcConnection, bytes []byte) (int, error) {
			if i == 0 {
				i += 1
				copy(bytes, encodedMsg)
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

		msg := &protocol.ChangeTopicMessage{
			Type:  protocol.MessageType_ADD_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.IsAuthenticated = false
		p.readReliablePump(&state)

		require.Len(t, state.topicQueue, 0)
		require.True(t, p.isClosed)
	})

	t.Run("sending data before authentication (unreliable)", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.ChangeTopicMessage{
			Type:  protocol.MessageType_ADD_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.IsAuthenticated = false
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
		p.IsAuthenticated = false
		p.readReliablePump(&state)

		require.True(t, p.isClosed)
		require.False(t, p.IsAuthenticated)
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
		p.IsAuthenticated = false
		p.readReliablePump(&state)

		require.True(t, p.isClosed)
		require.True(t, p.IsAuthenticated)
		require.False(t, p.isServer)
	})

	t.Run("server authentication accepted", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		state.subscriptions["topic1"] = true
		state.subscriptions["topic2"] = true
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
		p.IsAuthenticated = false
		p.readReliablePump(&state)

		require.True(t, authMessageGenerated)
		require.True(t, p.isClosed)
		require.True(t, p.IsAuthenticated)
		require.True(t, p.isServer)

		require.Len(t, p.sendReliable, 3)
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
		p.IsAuthenticated = false
		p.readReliablePump(&state)

		require.True(t, p.isClosed)
		require.False(t, p.IsAuthenticated)
		require.False(t, p.isServer)
	})

	t.Run("add topic message", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.ChangeTopicMessage{
			Type:  protocol.MessageType_ADD_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.readReliablePump(&state)

		require.Len(t, state.topicQueue, 1)
		change := <-state.topicQueue
		require.Equal(t, AddTopic, change.op)
		require.Equal(t, "peer1", change.alias)
		require.Equal(t, "topic1", change.topic)
	})

	t.Run("remove topic message", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		msg := &protocol.ChangeTopicMessage{
			Type:  protocol.MessageType_REMOVE_TOPIC,
			Topic: "topic1",
		}

		p := setupPeer(t, "peer1", msg)
		p.readReliablePump(&state)

		require.Len(t, state.topicQueue, 1)
		change := <-state.topicQueue
		require.Equal(t, RemoveTopic, change.op)
		require.Equal(t, "peer1", change.alias)
		require.Equal(t, "topic1", change.topic)
	})

	t.Run("reliable topic message", func(t *testing.T) {
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
		require.Equal(t, "peer1", peerMessage.from)
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
		require.Equal(t, "peer1", peerMessage.from)
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
	t.Run("no peer", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		state.topicQueue <- &topicChange{
			op:    AddTopic,
			alias: "peer1",
			topic: "topic1",
		}

		state.softStop = true

		Process(&state)

		require.Len(t, state.peersIndex, 0)
	})

	t.Run("add topic from clients", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		p1 := addPeer(&state, makeClient("peer1", nil))
		p2 := addPeer(&state, makeClient("peer2", nil))
		p3 := addPeer(&state, makeServer("peer3", nil))

		state.topicQueue <- &topicChange{
			op:    AddTopic,
			alias: "peer1",
			topic: "topic1",
		}
		state.topicQueue <- &topicChange{
			op:    AddTopic,
			alias: "peer2",
			topic: "topic1",
		}
		state.softStop = true

		Process(&state)

		require.Len(t, state.peersIndex, 1)
		require.Contains(t, state.peersIndex, "topic1")

		// NOTE server should subscribe to topic and send the add topic message to
		// the others connected servers
		require.Len(t, state.subscriptions, 1)
		require.Contains(t, state.subscriptions, "topic1")
		require.Len(t, p3.sendReliable, 1)

		require.Contains(t, state.peersIndex["topic1"], "peer1")
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

		state.topicQueue <- &topicChange{
			op:    AddTopic,
			alias: "peer1",
			topic: "topic1",
		}
		state.softStop = true

		Process(&state)

		require.Len(t, state.peersIndex, 1)
		require.Contains(t, state.peersIndex, "topic1")

		// NOTE: no subscriptions from server
		require.Len(t, state.subscriptions, 0)
		require.Len(t, p2.sendReliable, 0)

		require.Contains(t, state.peersIndex["topic1"], "peer1")
		require.Len(t, p1.Topics, 1)
		require.Contains(t, p1.Topics, "topic1")
	})

	t.Run("remove topic from clients", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		state.subscriptions["topic1"] = true

		p1 := addPeer(&state, makeClient("peer1", nil))
		p1.Topics["topic1"] = true

		p2 := addPeer(&state, makeClient("peer2", nil))
		p2.Topics["topic1"] = true

		p3 := addPeer(&state, makeServer("peer3", nil))

		state.peersIndex["topic1"] = map[string]bool{"peer1": true, "peer2": true}

		state.topicQueue <- &topicChange{
			op:    RemoveTopic,
			alias: "peer1",
			topic: "topic1",
		}

		state.topicQueue <- &topicChange{
			op:    RemoveTopic,
			alias: "peer2",
			topic: "topic1",
		}

		state.softStop = true

		Process(&state)

		// NOTE server should unsubscribe to topic and send the remove topic message to
		// the others connected servers
		require.Len(t, state.subscriptions, 0)
		require.Len(t, p3.sendReliable, 1)

		require.Len(t, state.peersIndex, 1)
		require.Contains(t, state.peersIndex, "topic1")
		require.Len(t, state.peersIndex["topic1"], 0)
		require.Len(t, p1.Topics, 0)
	})
}

func TestUnregister(t *testing.T) {
	state := makeTestState(t)
	defer closeState(&state)

	p := addPeer(&state, makeClient("peer1", nil))
	p.Topics["topic1"] = true

	p2 := addPeer(&state, makeClient("peer2", nil))
	p2.Topics["topic1"] = true

	state.peersIndex["topic1"] = map[string]bool{"peer1": true, "peer2": true}

	state.unregisterQueue <- p.alias
	state.unregisterQueue <- p2.alias
	state.softStop = true

	Process(&state)

	require.Len(t, state.Peers, 0)
	require.Len(t, state.peersIndex, 1)
	require.Len(t, state.peersIndex["topic1"], 0)
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
		require.Contains(t, state.Peers, "peer1")
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
			FromAlias: p.alias,
		}

		state.webRtcControlQueue <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Sdp:       "sdp-offer",
			FromAlias: p2.alias,
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
			FromAlias: p.alias,
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
			FromAlias: p.alias,
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
			FromAlias: p.alias,
		}

		state.softStop = true

		Process(&state)

		require.Equal(t, 1, i)
	})
}

func TestProcessPeerMessage(t *testing.T) {
	t.Run("no peers", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		state.messagesQueue <- &peerMessage{
			topic: "topic1",
			from:  "peer1",
			bytes: []byte{},
		}
	})

	t.Run("success", func(t *testing.T) {
		state := makeTestState(t)
		defer closeState(&state)

		conn1 := makeMockWebRtcConnection()
		p1 := addPeer(&state, makeClient("peer1", conn1))
		p1.Topics["topic1"] = true

		conn2 := makeMockWebRtcConnection()
		p2 := addPeer(&state, makeClient("peer2", conn2))
		p2.Topics["topic1"] = true

		conn3 := makeMockWebRtcConnection()
		p3 := addPeer(&state, makeClient("peer3", conn3))

		conn4 := makeMockWebRtcConnection()
		p4 := addPeer(&state, makeClient("peer4", conn4))
		p4.Topics["topic1"] = true
		p4.isClosed = true

		state.peersIndex["topic1"] = map[string]bool{
			"peer1": true,
			"peer2": true,
			"peer4": true,
		}

		bytes, err := proto.Marshal(&protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		})
		require.NoError(t, err)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     "peer1",
			bytes:    bytes,
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
		p1.Topics["topic1"] = true

		conn2 := makeMockWebRtcConnection()
		p2 := addPeer(&state, makeClient("peer2", conn2))
		p2.Topics["topic1"] = true

		state.peersIndex["topic1"] = map[string]bool{
			"peer1": true,
			"peer2": true,
		}

		bytes, err := proto.Marshal(&protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "topic1",
		})
		require.NoError(t, err)

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     "peer1",
			bytes:    bytes,
		}

		state.messagesQueue <- &peerMessage{
			reliable: true,
			topic:    "topic1",
			from:     "peer1",
			bytes:    bytes,
		}

		state.softStop = true

		Process(&state)

		require.Len(t, p1.sendReliable, 0) // NOTE don't send it to itself
		require.Len(t, p2.sendReliable, 2) // NOTE p2 is ready and it's listening to the topic
	})
}
