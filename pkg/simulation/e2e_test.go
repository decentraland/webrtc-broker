// +build integration

package simulation

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"

	"github.com/decentraland/webrtc-broker/pkg/broker"
	"github.com/decentraland/webrtc-broker/pkg/coordinator"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const (
	sleepPeriod     = 9 * time.Second
	longSleepPeriod = sleepPeriod * 2
)

var testLogLevel = zerolog.InfoLevel
var clientLogLevel = zerolog.WarnLevel
var serverLogLevel = zerolog.WarnLevel
var serverWebRtcLogLevel = zerolog.ErrorLevel
var coordinatorLogLevel = zerolog.WarnLevel

type PeerWriter = broker.PeerWriter
type WriterController = broker.WriterController

func printTitle(log zerolog.Logger, title string) {
	log.Info().Msgf("=== %s ===", title)
}

func startCoordinator(t *testing.T, addr string) (*coordinator.State, *http.Server, string) {
	config := &coordinator.Config{}
	return startCoordinatorWithConfig(t, addr, config)
}

func startCoordinatorWithConfig(t *testing.T, addr string, config *coordinator.Config) (*coordinator.State, *http.Server, string) {
	log := logging.New().Level(coordinatorLogLevel)
	config.Auth = &authentication.NoopAuthenticator{}
	config.Log = &log

	state := coordinator.MakeState(config)

	go coordinator.Start(state)

	mux := http.NewServeMux()
	mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		qs := r.URL.Query()
		role := protocol.Role_COMMUNICATION_SERVER

		if qs.Get("role") == protocol.Role_COMMUNICATION_SERVER_HUB.String() {
			role = protocol.Role_COMMUNICATION_SERVER_HUB
		}

		ws, err := coordinator.UpgradeRequest(state, role, w, r)
		require.NoError(t, err)
		coordinator.ConnectCommServer(state, ws, role)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeRequest(state, protocol.Role_CLIENT, w, r)
		require.NoError(t, err)
		coordinator.ConnectClient(state, ws)
	})

	s := &http.Server{Addr: addr, Handler: mux}
	go func() {
		t.Log("Starting coordinator")
		s.ListenAndServe()
	}()

	return state, s, fmt.Sprintf("ws://%s", addr)
}

func startBroker(t *testing.T, coordinatorURL string, role protocol.Role) *broker.Broker {
	config := &broker.Config{CoordinatorURL: coordinatorURL, Role: role}
	return startBrokerWithConfig(t, config)
}

func startBrokerWithConfig(t *testing.T, config *broker.Config) *broker.Broker {
	config.Auth = &authentication.NoopAuthenticator{}
	config.WebRtcLogLevel = serverWebRtcLogLevel
	config.ExitOnCoordinatorClose = false
	log := logging.New().Level(serverLogLevel)
	config.Log = &log

	b, err := broker.NewBroker(config)
	require.NoError(t, err)
	t.Log("Starting communication server node")

	require.NoError(t, b.Connect())

	go b.ProcessSubscriptionChannel()
	go b.ProcessMessagesChannel()
	go b.ProcessControlMessages()
	return b
}

func start(t *testing.T, client *Client) peerData {
	go func() {
		require.NoError(t, client.startCoordination())
	}()

	return <-client.PeerData
}

type recvMessage struct {
	msgType protocol.MessageType
	raw     []byte
}

func makeReadableClient(t *testing.T, coordinatorURL string) (*Client, chan recvMessage, chan recvMessage) {
	receivedReliable := make(chan recvMessage, 256)
	receivedUnreliable := make(chan recvMessage, 256)

	config := Config{
		Auth:           &authentication.NoopAuthenticator{},
		CoordinatorURL: coordinatorURL,
		OnMessageReceived: func(reliable bool, msgType protocol.MessageType, raw []byte) {
			m := recvMessage{msgType: msgType, raw: raw}
			if reliable {
				receivedReliable <- m
			} else {
				receivedUnreliable <- m
			}
		},
		Log: logging.New().Level(clientLogLevel),
	}
	return MakeClient(&config), receivedReliable, receivedUnreliable
}

func TestSingleServerTopology(t *testing.T) {
	_, server, coordinatorURL := startCoordinator(t, "localhost:9997")
	defer server.Close()

	log := logging.New().Level(testLogLevel)

	printTitle(log, "Starting comm servers")
	broker1 := startBroker(t, coordinatorURL, protocol.Role_COMMUNICATION_SERVER)
	defer broker1.Shutdown()

	c1, c1ReceivedReliable, c1ReceivedUnreliable := makeReadableClient(t, coordinatorURL)
	c2, c2ReceivedReliable, c2ReceivedUnreliable := makeReadableClient(t, coordinatorURL)

	printTitle(log, "Starting client1")
	c1Data := start(t, c1)
	require.NoError(t, c1.Connect(c1Data.Alias, c1Data.AvailableServers[0]))
	log.Info().Msgf("client1 alias is %d", c1Data.Alias)

	printTitle(log, "Starting client2")
	c2Data := start(t, c2)
	require.NoError(t, c2.Connect(c2Data.Alias, c2Data.AvailableServers[0]))
	log.Info().Msgf("client2 alias is %d", c2Data.Alias)

	// NOTE: wait until connections are ready
	time.Sleep(sleepPeriod)

	broker1Stats := broker1.GetBrokerStats()
	require.NotEmpty(t, broker1.Alias)
	require.NotEmpty(t, c1Data.Alias)
	require.NotEmpty(t, c2Data.Alias)

	log.Info().Msgf("broker1 alias is %d", broker1Stats.Alias)

	require.Contains(t, broker1Stats.Peers, c1Data.Alias)
	require.Contains(t, broker1Stats.Peers, c2Data.Alias)
	require.Equal(t, 2, len(broker1Stats.Peers))

	printTitle(log, "Authorizing clients")
	authMessage := protocol.AuthMessage{
		Type: protocol.MessageType_AUTH,
		Role: protocol.Role_CLIENT,
	}
	authBytes, err := proto.Marshal(&authMessage)
	require.NoError(t, err)

	c1.authMessage <- authBytes
	c2.authMessage <- authBytes

	// NOTE: wait until connections are authenticated
	time.Sleep(longSleepPeriod)
	broker1Stats = broker1.GetBrokerStats()

	printTitle(log, "Both clients are subscribing to 'test' topic")
	require.NoError(t, c1.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))

	// NOTE: wait until subscriptions are ready
	time.Sleep(longSleepPeriod)

	printTitle(log, "Each client sends a topic message, by reliable channel")
	msg := protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: "test",
		Body:  []byte("c1 test"),
	}
	c1EncodedMessage, err := proto.Marshal(&msg)
	require.NoError(t, err)
	c1.SendReliable <- c1EncodedMessage

	msg = protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: "test",
		Body:  []byte("c2 test"),
	}
	c2EncodedMessage, err := proto.Marshal(&msg)
	require.NoError(t, err)
	c2.SendReliable <- c2EncodedMessage

	// NOTE wait until messages are received
	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedReliable, 1)
	require.Len(t, c2ReceivedReliable, 1)

	topicFWMessage := protocol.TopicFWMessage{}

	recvMsg := <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	printTitle(log, "Each client sends a topic message, by unreliable channel")
	c1.SendUnreliable <- c1EncodedMessage
	c2.SendUnreliable <- c2EncodedMessage

	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedUnreliable, 1)
	require.Len(t, c2ReceivedUnreliable, 1)

	recvMsg = <-c1ReceivedUnreliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedUnreliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	printTitle(log, "Remove topic")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{}))

	printTitle(log, "Testing webrtc connection close")
	c2.StopReliableQueue <- true
	c2.StopUnreliableQueue <- true
	go c2.conn.Close()
	c2.conn = nil
	c2.Connect(c2Data.Alias, broker1Stats.Alias)
	c2.authMessage <- authBytes
	time.Sleep(longSleepPeriod)

	printTitle(log, "Subscribe to topics again")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	time.Sleep(longSleepPeriod)

	printTitle(log, "Each client sends a topic message, by reliable channel")
	c1.SendReliable <- c1EncodedMessage
	c2.SendReliable <- c2EncodedMessage

	time.Sleep(sleepPeriod)
	require.Len(t, c1ReceivedReliable, 1)
	require.Len(t, c2ReceivedReliable, 1)

	recvMsg = <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	log.Info().Msg("TEST END")
}

func TestMeshTopology(t *testing.T) {
	_, server, coordinatorURL := startCoordinator(t, "localhost:9998")
	defer server.Close()

	log := logging.New().Level(testLogLevel)

	printTitle(log, "Starting comm servers")
	broker1 := startBroker(t, coordinatorURL, protocol.Role_COMMUNICATION_SERVER)
	defer broker1.Shutdown()

	broker2 := startBroker(t, coordinatorURL, protocol.Role_COMMUNICATION_SERVER)
	defer broker2.Shutdown()

	c1, c1ReceivedReliable, c1ReceivedUnreliable := makeReadableClient(t, coordinatorURL)
	c2, c2ReceivedReliable, c2ReceivedUnreliable := makeReadableClient(t, coordinatorURL)

	printTitle(log, "Starting client1")
	c1Data := start(t, c1)
	require.NoError(t, c1.Connect(c1Data.Alias, c1Data.AvailableServers[0]))
	log.Info().Msgf("client1 alias is %d", c1Data.Alias)

	printTitle(log, "Starting client2")
	c2Data := start(t, c2)
	require.NoError(t, c2.Connect(c2Data.Alias, c2Data.AvailableServers[1]))
	log.Info().Msgf("client2 alias is %d", c2Data.Alias)

	// NOTE: wait until connections are ready
	time.Sleep(sleepPeriod)

	broker1Stats := broker1.GetBrokerStats()
	broker2Stats := broker2.GetBrokerStats()
	require.NotEmpty(t, broker1Stats.Alias)
	require.NotEmpty(t, broker2Stats.Alias)
	require.NotEmpty(t, c1Data.Alias)
	require.NotEmpty(t, c2Data.Alias)

	log.Info().Msgf("broker1 alias is %d", broker1Stats.Alias)
	log.Info().Msgf("broker2 alias is %d", broker2Stats.Alias)

	require.Contains(t, broker1Stats.Peers, broker2Stats.Alias)
	require.Contains(t, broker2Stats.Peers, broker1Stats.Alias)
	require.Contains(t, broker1Stats.Peers, c1Data.Alias)
	require.Contains(t, broker2Stats.Peers, c2Data.Alias)
	require.Equal(t, 4, len(broker1Stats.Peers)+len(broker2Stats.Peers))

	printTitle(log, "Authorizing clients")
	authMessage := protocol.AuthMessage{
		Type: protocol.MessageType_AUTH,
		Role: protocol.Role_CLIENT,
	}
	authBytes, err := proto.Marshal(&authMessage)
	require.NoError(t, err)

	c1.authMessage <- authBytes
	c2.authMessage <- authBytes

	// NOTE: wait until connections are authenticated
	time.Sleep(longSleepPeriod)
	broker1Stats = broker1.GetBrokerStats()
	broker2Stats = broker2.GetBrokerStats()

	printTitle(log, "Both clients are subscribing to 'test' topic")
	require.NoError(t, c1.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))

	// NOTE: wait until subscriptions are ready
	time.Sleep(longSleepPeriod)

	printTitle(log, "Each client sends a topic message, by reliable channel")
	msg := protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: "test",
		Body:  []byte("c1 test"),
	}
	c1EncodedMessage, err := proto.Marshal(&msg)
	require.NoError(t, err)
	c1.SendReliable <- c1EncodedMessage

	msg = protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: "test",
		Body:  []byte("c2 test"),
	}
	c2EncodedMessage, err := proto.Marshal(&msg)
	require.NoError(t, err)
	c2.SendReliable <- c2EncodedMessage

	// NOTE wait until messages are received
	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedReliable, 1)
	require.Len(t, c2ReceivedReliable, 1)

	topicFWMessage := protocol.TopicFWMessage{}

	recvMsg := <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	printTitle(log, "Each client sends a topic message, by unreliable channel")
	c1.SendUnreliable <- c1EncodedMessage
	c2.SendUnreliable <- c2EncodedMessage

	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedUnreliable, 1)
	require.Len(t, c2ReceivedUnreliable, 1)

	recvMsg = <-c1ReceivedUnreliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedUnreliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	printTitle(log, "Remove topic")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{}))

	printTitle(log, "Testing webrtc connection close")
	c2.StopReliableQueue <- true
	c2.StopUnreliableQueue <- true
	go c2.conn.Close()
	c2.conn = nil
	c2.Connect(c2Data.Alias, broker1Stats.Alias)
	c2.authMessage <- authBytes
	time.Sleep(longSleepPeriod)

	printTitle(log, "Subscribe to topics again")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	time.Sleep(longSleepPeriod)

	printTitle(log, "Each client sends a topic message, by reliable channel")
	c1.SendReliable <- c1EncodedMessage
	c2.SendReliable <- c2EncodedMessage

	time.Sleep(sleepPeriod)
	require.Len(t, c1ReceivedReliable, 1)
	require.Len(t, c2ReceivedReliable, 1)

	recvMsg = <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	log.Info().Msg("TEST END")
}

type starTopologyServerSelector struct {
	HubAlias      uint64
	ServerAliases map[uint64]bool
}

func (s *starTopologyServerSelector) ServerRegistered(role protocol.Role, alias uint64) {
	if role == protocol.Role_COMMUNICATION_SERVER_HUB {
		s.HubAlias = alias
	} else {
		s.ServerAliases[alias] = true
	}
}

func (s *starTopologyServerSelector) GetServerAliasList(forRole protocol.Role) []uint64 {
	if forRole == protocol.Role_CLIENT || forRole == protocol.Role_COMMUNICATION_SERVER_HUB {
		peers := make([]uint64, 0, len(s.ServerAliases))

		for alias := range s.ServerAliases {
			peers = append(peers, alias)
		}

		sort.Sort(coordinator.ByAlias(peers))
		return peers
	}

	if forRole == protocol.Role_COMMUNICATION_SERVER && s.HubAlias > 0 {
		peers := make([]uint64, 1)
		peers[0] = s.HubAlias
		return peers
	}

	return []uint64{}
}

func (s *starTopologyServerSelector) ServerUnregistered(alias uint64) {
}

func (s *starTopologyServerSelector) GetServerCount() int {
	count := len(s.ServerAliases)
	if s.HubAlias > 0 {
		count++
	}
	return count
}

func TestStarTopology(t *testing.T) {
	config := &coordinator.Config{}
	config.ServerSelector = &starTopologyServerSelector{
		ServerAliases: make(map[uint64]bool),
	}
	_, server, coordinatorURL := startCoordinatorWithConfig(t, "localhost:9998", config)
	defer server.Close()

	log := logging.New().Level(testLogLevel)

	printTitle(log, "Starting comm servers")
	hub := startBroker(t, coordinatorURL, protocol.Role_COMMUNICATION_SERVER_HUB)
	defer hub.Shutdown()

	broker1 := startBroker(t, coordinatorURL, protocol.Role_COMMUNICATION_SERVER)
	defer broker1.Shutdown()

	broker2 := startBroker(t, coordinatorURL, protocol.Role_COMMUNICATION_SERVER)
	defer broker2.Shutdown()

	c1, c1ReceivedReliable, c1ReceivedUnreliable := makeReadableClient(t, coordinatorURL)
	c2, c2ReceivedReliable, c2ReceivedUnreliable := makeReadableClient(t, coordinatorURL)

	printTitle(log, "Starting client1")
	c1Data := start(t, c1)
	require.NoError(t, c1.Connect(c1Data.Alias, c1Data.AvailableServers[0]))
	log.Info().Msgf("client1 alias is %d", c1Data.Alias)

	printTitle(log, "Starting client2")
	c2Data := start(t, c2)
	require.NoError(t, c2.Connect(c2Data.Alias, c2Data.AvailableServers[1]))
	log.Info().Msgf("client2 alias is %d", c2Data.Alias)

	// NOTE: wait until connections are ready
	time.Sleep(sleepPeriod)

	hubStats := hub.GetBrokerStats()
	broker1Stats := broker1.GetBrokerStats()
	broker2Stats := broker2.GetBrokerStats()
	require.NotEmpty(t, broker1Stats.Alias)
	require.NotEmpty(t, broker2Stats.Alias)
	require.NotEmpty(t, c1Data.Alias)
	require.NotEmpty(t, c2Data.Alias)

	log.Info().Msgf("hub alias is %d", hubStats.Alias)
	log.Info().Msgf("broker1 alias is %d", broker1Stats.Alias)
	log.Info().Msgf("broker2 alias is %d", broker2Stats.Alias)

	require.Contains(t, broker1Stats.Peers, hubStats.Alias)
	require.Contains(t, broker2Stats.Peers, hubStats.Alias)
	require.Contains(t, broker1Stats.Peers, c1Data.Alias)
	require.Contains(t, broker2Stats.Peers, c2Data.Alias)
	require.Equal(t, 4, len(broker1Stats.Peers)+len(broker2Stats.Peers))

	printTitle(log, "Authorizing clients")
	authMessage := protocol.AuthMessage{
		Type: protocol.MessageType_AUTH,
		Role: protocol.Role_CLIENT,
	}
	authBytes, err := proto.Marshal(&authMessage)
	require.NoError(t, err)

	c1.authMessage <- authBytes
	c2.authMessage <- authBytes

	// NOTE: wait until connections are authenticated
	time.Sleep(longSleepPeriod)

	printTitle(log, "Both clients are subscribing to 'test' topic")
	require.NoError(t, c1.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))

	// NOTE: wait until subscriptions are ready
	time.Sleep(longSleepPeriod)

	printTitle(log, "Each client sends a topic message, by reliable channel")
	msg := protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: "test",
		Body:  []byte("c1 test"),
	}
	c1EncodedMessage, err := proto.Marshal(&msg)
	require.NoError(t, err)
	c1.SendReliable <- c1EncodedMessage

	msg = protocol.TopicMessage{
		Type:  protocol.MessageType_TOPIC,
		Topic: "test",
		Body:  []byte("c2 test"),
	}
	c2EncodedMessage, err := proto.Marshal(&msg)
	require.NoError(t, err)
	c2.SendReliable <- c2EncodedMessage

	// NOTE wait until messages are received
	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedReliable, 1)
	require.Len(t, c2ReceivedReliable, 1)

	topicFWMessage := protocol.TopicFWMessage{}

	recvMsg := <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	printTitle(log, "Each client sends a topic message, by unreliable channel")
	c1.SendUnreliable <- c1EncodedMessage
	c2.SendUnreliable <- c2EncodedMessage

	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedUnreliable, 1)
	require.Len(t, c2ReceivedUnreliable, 1)

	recvMsg = <-c1ReceivedUnreliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c2 test"), topicFWMessage.Body)
	require.Equal(t, c2Data.Alias, topicFWMessage.FromAlias)

	recvMsg = <-c2ReceivedUnreliable
	require.Equal(t, protocol.MessageType_TOPIC_FW, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &topicFWMessage))
	require.Equal(t, []byte("c1 test"), topicFWMessage.Body)
	require.Equal(t, c1Data.Alias, topicFWMessage.FromAlias)

	log.Info().Msg("TEST END")
}

type session struct {
	coordinator *http.Server
	broker      *broker.Broker

	c1, c2           *Client
	c1Alias, c2Alias uint64
}

func TestControlFlow(t *testing.T) {
	prepare := func(t *testing.T, log logging.Logger, brokerConfig *broker.Config) session {
		var s session
		_, server, coordinatorURL := startCoordinator(t, "localhost:9999")
		s.coordinator = server

		printTitle(log, "Starting comm server")
		brokerConfig.CoordinatorURL = coordinatorURL
		brokerConfig.Role = protocol.Role_COMMUNICATION_SERVER
		s.broker = startBrokerWithConfig(t, brokerConfig)

		auth := &authentication.NoopAuthenticator{}

		config := Config{
			Auth:           auth,
			CoordinatorURL: coordinatorURL,
			Log:            logging.New().Level(clientLogLevel),
		}
		s.c1 = MakeClient(&config)

		config = Config{
			Auth:           auth,
			CoordinatorURL: coordinatorURL,
			Log:            logging.New().Level(clientLogLevel),
		}
		s.c2 = MakeClient(&config)

		printTitle(log, "Starting client1")
		c1Data := start(t, s.c1)
		require.NoError(t, s.c1.Connect(c1Data.Alias, c1Data.AvailableServers[0]))

		printTitle(log, "Starting client2")
		c2Data := start(t, s.c2)
		require.NoError(t, s.c2.Connect(c2Data.Alias, c2Data.AvailableServers[0]))

		// NOTE: wait until connections are ready
		time.Sleep(sleepPeriod)

		brokerStats := s.broker.GetBrokerStats()
		require.NotEmpty(t, brokerStats.Alias)
		require.NotEmpty(t, c1Data.Alias)
		require.NotEmpty(t, c2Data.Alias)

		printTitle(log, "Aliases")
		log.Info().Msgf("broker alias is %d", brokerStats.Alias)
		log.Info().Msgf("client1 alias is %d", c1Data.Alias)
		log.Info().Msgf("client2 alias is %d", c2Data.Alias)
		s.c1Alias = c1Data.Alias
		s.c2Alias = c2Data.Alias

		require.Equal(t, 2, len(brokerStats.Peers))

		printTitle(log, "Authorizing clients")
		authMessage := protocol.AuthMessage{
			Type: protocol.MessageType_AUTH,
			Role: protocol.Role_CLIENT,
		}
		authBytes, err := proto.Marshal(&authMessage)
		require.NoError(t, err)

		s.c1.authMessage <- authBytes
		s.c2.authMessage <- authBytes

		// NOTE: wait until connections are authenticated
		time.Sleep(longSleepPeriod)

		printTitle(log, "Both clients are subscribing to 'test' topic")
		require.NoError(t, s.c1.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
		require.NoError(t, s.c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))

		// NOTE: wait until subscriptions are ready
		time.Sleep(sleepPeriod)

		return s
	}

	t.Run("fixed queue controller", func(t *testing.T) {
		log := logging.New().Level(testLogLevel)

		var c2UnreliableWriter *broker.FixedQueueWriterController
		s := prepare(t, log, &broker.Config{
			UnreliableWriterControllerFactory: func(alias uint64, writer PeerWriter) WriterController {
				w := broker.NewFixedQueueWriterController(writer, 100, 200)
				if alias == 3 {
					c2UnreliableWriter = w
				}
				return w
			},
		})
		defer s.coordinator.Close()
		defer s.broker.Shutdown()

		msg := protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "test",
			Body:  make([]byte, 100),
		}
		encodedMessage, err := proto.Marshal(&msg)
		require.NoError(t, err)

		messageCount := uint32(10000)
		go func(messageCount uint32) {
			for i := uint32(0); i < messageCount; i++ {
				s.c1.SendUnreliable <- encodedMessage
			}
		}(messageCount)

		for {
			client2Stats := s.broker.GetBrokerStats().Peers[s.c2Alias]
			unreliableBufferedAmount := client2Stats.UnreliableBufferedAmount
			unreliableMessagesSent := client2Stats.UnreliableMessagesSent
			discardedCount := c2UnreliableWriter.GetDiscardedCount()
			log.Debug().
				Uint64("unreliable buffered amount", unreliableBufferedAmount).
				Uint32("unreliable messages sent", unreliableMessagesSent).
				Uint32("unreliable messages discarded", discardedCount).
				Msg("buffered amount")

			require.LessOrEqual(t, unreliableBufferedAmount, uint64(150))
			if (unreliableMessagesSent + discardedCount) == messageCount {
				break
			}

			time.Sleep(10 * time.Millisecond)
		}

		log.Info().Msg("TEST END")
	})

	t.Run("discard writer controller", func(t *testing.T) {
		log := logging.New().Level(testLogLevel)

		var c2UnreliableWriter *broker.DiscardWriterController
		s := prepare(t, log, &broker.Config{
			UnreliableWriterControllerFactory: func(alias uint64, writer PeerWriter) WriterController {
				w := broker.NewDiscardWriterController(writer, 200)
				if alias == 3 {
					c2UnreliableWriter = w
				}
				return w
			},
		})
		defer s.coordinator.Close()
		defer s.broker.Shutdown()

		msg := protocol.TopicMessage{
			Type:  protocol.MessageType_TOPIC,
			Topic: "test",
			Body:  make([]byte, 100),
		}
		encodedMessage, err := proto.Marshal(&msg)
		require.NoError(t, err)

		messageCount := uint32(10000)
		go func(messageCount uint32) {
			for i := uint32(0); i < messageCount; i++ {
				s.c1.SendUnreliable <- encodedMessage
			}
		}(messageCount)

		tolerance := 32
		for {
			client2Stats := s.broker.GetBrokerStats().Peers[s.c2Alias]
			unreliableBufferedAmount := client2Stats.UnreliableBufferedAmount
			unreliableMessagesSent := client2Stats.UnreliableMessagesSent
			discardedCount := c2UnreliableWriter.GetDiscardedCount()
			log.Debug().
				Uint64("unreliable buffered amount", unreliableBufferedAmount).
				Uint32("unreliable messages sent", unreliableMessagesSent).
				Uint32("unreliable messages discarded", discardedCount).
				Msg("buffered amount")

			require.LessOrEqual(t, unreliableBufferedAmount, uint64(100+tolerance))
			if (unreliableMessagesSent + discardedCount) == messageCount {
				break
			}

			time.Sleep(10 * time.Millisecond)
		}

		log.Info().Msg("TEST END")
	})
}
