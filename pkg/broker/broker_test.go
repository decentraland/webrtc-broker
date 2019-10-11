package broker

import (
	"testing"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/decentraland/webrtc-broker/pkg/server"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockWriterController struct {
	mock.Mock
}

func (m *mockWriterController) Write(p []byte) {
	m.Called(p)
}

func (m *mockWriterController) OnBufferedAmountLow() {
	m.Called()
}

const (
	clientRole int32 = int32(protocol.Role_CLIENT)
	serverRole int32 = int32(protocol.Role_COMMUNICATION_SERVER)
)

func TestTopicSubscriptions(t *testing.T) {
	t.Run("add client subscription", func(t *testing.T) {
		c1 := &peer{role: clientRole}
		c2 := &peer{role: clientRole}
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
		s1 := &peer{role: serverRole}
		s2 := &peer{role: serverRole}
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
		c1 := &peer{role: clientRole}
		c2 := &peer{role: clientRole}
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
		s1 := &peer{role: serverRole}
		s2 := &peer{role: serverRole}
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
		c1 := &peer{role: clientRole}
		s1 := &peer{role: serverRole}
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
		c1 := &peer{role: clientRole}
		s1 := &peer{role: serverRole}
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

func TestProcessTopicMessage(t *testing.T) {
	b, err := NewBroker(&Config{
		Role: protocol.Role_COMMUNICATION_SERVER,
		Auth: &authentication.NoopAuthenticator{},
	})
	require.NoError(t, err)

	serverReliableWriter := mockWriterController{}
	serverReliableWriter.On("Write", mock.Anything).Return().Once()

	log := logging.New()

	b.peers[1] = &peer{
		Peer:           &server.Peer{Log: log},
		role:           serverRole,
		topics:         make(map[string]struct{}),
		reliableWriter: &serverReliableWriter,
	}

	c1ReliableWriter := mockWriterController{}
	b.peers[2] = &peer{
		role:           clientRole,
		topics:         make(map[string]struct{}),
		reliableWriter: &c1ReliableWriter,
	}

	c2ReliableWriter := mockWriterController{}
	c2ReliableWriter.On("Write", mock.Anything).Return().Once()

	b.peers[3] = &peer{
		role:           clientRole,
		topics:         make(map[string]struct{}),
		reliableWriter: &c2ReliableWriter,
	}

	b.subscriptions.AddServerSubscription("topic1", b.peers[1])
	b.subscriptions.AddClientSubscription("topic1", b.peers[2])
	b.subscriptions.AddClientSubscription("topic1", b.peers[3])

	b.processTopicMessage(&peerMessage{
		reliable:       true,
		topic:          "topic1",
		from:           b.peers[2],
		rawMsgToClient: make([]byte, 10),
	})

	serverReliableWriter.AssertExpectations(t)
	c1ReliableWriter.AssertExpectations(t)
	c2ReliableWriter.AssertExpectations(t)
}

func TestProcessSubscriptionChange(t *testing.T) {
	b, err := NewBroker(&Config{
		Role: protocol.Role_COMMUNICATION_SERVER,
		Auth: &authentication.NoopAuthenticator{},
	})
	require.NoError(t, err)

	serverReliableWriter := mockWriterController{}
	serverReliableWriter.On("Write", mock.Anything).Return().Twice()

	log := logging.New()

	b.peers[1] = &peer{
		Peer:           &server.Peer{Log: log},
		role:           serverRole,
		topics:         make(map[string]struct{}),
		reliableWriter: &serverReliableWriter,
	}

	c1 := &peer{
		role:   clientRole,
		topics: make(map[string]struct{}),
	}

	require.NoError(t, b.processSubscriptionChange(subscriptionChange{
		peer:      c1,
		format:    protocol.Format_PLAIN,
		rawTopics: []byte("topic1"),
	}))

	require.Len(t, b.subscriptions, 1)
	require.Contains(t, b.subscriptions, "topic1")
	require.Len(t, b.subscriptions["topic1"].clients, 1)
	require.Len(t, b.subscriptions["topic1"].servers, 0)
	require.Contains(t, b.subscriptions["topic1"].clients, c1)

	require.NoError(t, b.processSubscriptionChange(subscriptionChange{
		peer:      c1,
		format:    protocol.Format_PLAIN,
		rawTopics: []byte(""),
	}))

	require.Len(t, b.subscriptions, 0)
	serverReliableWriter.AssertExpectations(t)
}

func TestOnPeerDisconnected(t *testing.T) {
	p := &peer{topics: map[string]struct{}{"topic1": {}}}
	p2 := &peer{topics: map[string]struct{}{"topic1": {}}}

	b, err := NewBroker(&Config{
		Role: protocol.Role_COMMUNICATION_SERVER,
		Auth: &authentication.NoopAuthenticator{},
	})
	require.NoError(t, err)

	b.peers[1] = p
	b.peers[2] = p2

	b.subscriptions.AddClientSubscription("topic1", p)
	b.subscriptions.AddClientSubscription("topic1", p2)

	require.Len(t, b.peers, 2)
	require.Len(t, b.subscriptions, 1)

	b.onPeerDisconnected(&server.Peer{Alias: 1})

	require.Len(t, b.peers, 1)
	require.Len(t, b.subscriptions, 1)

	b.onPeerDisconnected(&server.Peer{Alias: 2})

	require.Len(t, b.peers, 0)
	require.Len(t, b.subscriptions, 0)
}
