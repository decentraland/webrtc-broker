package worldcomm

import (
	"testing"

	"github.com/decentraland/communications-server-go/internal/agent"
	_testing "github.com/decentraland/communications-server-go/internal/testing"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

type MockWebsocket = _testing.MockWebsocket

func makeMockWebsocket() *MockWebsocket {
	return _testing.MakeMockWebsocket()
}

func TestCoordinatorSend(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := MakeState(agent, "noop", "")
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
		agent, err := agent.Make(appName, "")
		require.NoError(t, err)
		state := MakeState(agent, "noop", "")

		auth := &MockAuthenticator{
			GenerateAuthMessage_: func(role protocol.Role) (*protocol.AuthMessage, error) {
				require.Equal(t, role, protocol.Role_COMMUNICATION_SERVER)
				return &protocol.AuthMessage{}, nil
			},
		}

		state.Auth.AddOrUpdateAuthenticator("noop", auth)
		conn := makeMockWebsocket()
		state.coordinator.conn = conn
		return state, conn
	}

	t.Run("welcome server message", func(t *testing.T) {
		state, conn := setup()
		msg := &protocol.WelcomeServerMessage{
			Type:  protocol.MessageType_WELCOME_SERVER,
			Alias: "server3",
			Peers: []string{"server1", "server2"},
		}
		require.NoError(t, conn.PrepareToRead(msg))
		go state.coordinator.readPump(&state)

		alias := <-state.aliasChannel
		<-state.stop
		require.Len(t, state.coordinator.send, 2)

		connectMessage := &protocol.ConnectMessage{}
		require.Equal(t, alias, "server3")
		require.NoError(t, proto.Unmarshal(<-state.coordinator.send, connectMessage))
		require.Equal(t, connectMessage.ToAlias, "server1")
		require.NoError(t, proto.Unmarshal(<-state.coordinator.send, connectMessage))
		require.Equal(t, connectMessage.ToAlias, "server2")
	})

	t.Run("webrtc message", func(t *testing.T) {
		state, conn := setup()
		msg := &protocol.WebRtcMessage{
			Type: protocol.MessageType_WEBRTC_ANSWER,
		}
		require.NoError(t, conn.PrepareToRead(msg))
		go state.coordinator.readPump(&state)

		<-state.stop
		require.Len(t, state.webRtcControlQueue, 1)
	})

	t.Run("connect message", func(t *testing.T) {
		state, conn := setup()
		msg := &protocol.ConnectMessage{
			Type:      protocol.MessageType_CONNECT,
			FromAlias: "server2",
		}
		require.NoError(t, conn.PrepareToRead(msg))
		go state.coordinator.readPump(&state)

		<-state.stop
		require.Len(t, state.connectQueue, 1)
		require.Equal(t, <-state.connectQueue, "server2")
	})
}

func TestCoordinatorWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.PingMessage{})
	require.NoError(t, err)

	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := MakeState(agent, "noop", "")
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
