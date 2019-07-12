// +build integration

package simulation

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"

	"github.com/decentraland/webrtc-broker/pkg/commserver"
	"github.com/decentraland/webrtc-broker/pkg/coordinator"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const (
	sleepPeriod     = 5 * time.Second
	longSleepPeriod = 15 * time.Second
)

func printTitle(title string) {
	s := fmt.Sprintf("=== %s ===", title)
	fmt.Println(s)
}

func startCoordinator(t *testing.T) (*coordinator.State, *http.Server, string) {

	auth := &authentication.NoopAuthenticator{}
	config := coordinator.Config{
		ServerSelector: &coordinator.DefaultServerSelector{
			ServerAliases: make(map[uint64]bool),
		},
		Auth: auth,
	}
	state := coordinator.MakeState(&config)

	go coordinator.Start(state)

	mux := http.NewServeMux()
	mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeRequest(state, protocol.Role_COMMUNICATION_SERVER, w, r)
		require.NoError(t, err)
		coordinator.ConnectCommServer(state, ws)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeRequest(state, protocol.Role_CLIENT, w, r)
		require.NoError(t, err)
		coordinator.ConnectClient(state, ws)
	})

	addr := "localhost:9999"
	s := &http.Server{Addr: addr, Handler: mux}
	go func() {
		t.Log("starting coordinator")
		s.ListenAndServe()
	}()

	return state, s, fmt.Sprintf("ws://%s", addr)
}

type peerSnapshot struct {
	Topics map[string]bool
}

type commServerSnapshot struct {
	Alias uint64
	Peers []commserver.PeerStats
}

type testReporter struct {
	RequestData chan bool
	Data        chan commServerSnapshot
}

func (r *testReporter) Report(stats commserver.Stats) {
	select {
	case <-r.RequestData:
		snapshot := commServerSnapshot{
			Alias: stats.Alias,
			Peers: stats.Peers,
		}
		r.Data <- snapshot
	default:
	}
}

func (r *testReporter) GetStateSnapshot() commServerSnapshot {
	r.RequestData <- true
	return <-r.Data
}

func startCommServer(t *testing.T, coordinatorURL string) *testReporter {
	logger := logging.New().Level(zerolog.DebugLevel)
	auth := &authentication.NoopAuthenticator{}

	reporter := &testReporter{
		RequestData: make(chan bool),
		Data:        make(chan commServerSnapshot),
	}

	config := commserver.Config{
		Auth:           auth,
		Log:            &logger,
		CoordinatorURL: coordinatorURL,
		ReportPeriod:   1 * time.Second,
		Reporter:       func(stats commserver.Stats) { reporter.Report(stats) },
	}

	ws, err := commserver.MakeState(&config)
	require.NoError(t, err)
	t.Log("starting communication server node")

	require.NoError(t, commserver.ConnectCoordinator(ws))
	go commserver.ProcessMessagesQueue(ws)
	go commserver.Process(ws)
	return reporter
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

func TestE2E(t *testing.T) {
	_, server, coordinatorURL := startCoordinator(t)
	defer server.Close()

	topicFWMessage := protocol.TopicFWMessage{}

	printTitle("starting comm servers")
	comm1Reporter := startCommServer(t, coordinatorURL)
	comm2Reporter := startCommServer(t, coordinatorURL)

	auth := &authentication.NoopAuthenticator{}

	c1ReceivedReliable := make(chan recvMessage, 256)
	c1ReceivedUnreliable := make(chan recvMessage, 256)

	log := logging.New()
	config := Config{
		Auth:           auth,
		CoordinatorURL: coordinatorURL,
		OnMessageReceived: func(reliable bool, msgType protocol.MessageType, raw []byte) {
			m := recvMessage{msgType: msgType, raw: raw}
			if reliable {
				c1ReceivedReliable <- m
			} else {
				c1ReceivedUnreliable <- m
			}
		},
		Log: log,
	}
	c1 := MakeClient(&config)

	c2ReceivedReliable := make(chan recvMessage, 256)
	c2ReceivedUnreliable := make(chan recvMessage, 256)
	config = Config{
		Auth:           auth,
		CoordinatorURL: coordinatorURL,
		OnMessageReceived: func(reliable bool, msgType protocol.MessageType, raw []byte) {
			m := recvMessage{msgType: msgType, raw: raw}
			if reliable {
				c2ReceivedReliable <- m
			} else {
				c2ReceivedUnreliable <- m
			}
		},
		Log: log,
	}
	c2 := MakeClient(&config)

	printTitle("Starting client1")
	c1Data := start(t, c1)
	require.NoError(t, c1.Connect(c1Data.Alias, c1Data.AvailableServers[0]))

	printTitle("Starting client2")
	c2Data := start(t, c2)
	require.NoError(t, c2.Connect(c2Data.Alias, c2Data.AvailableServers[1]))

	// NOTE: wait until connections are ready
	time.Sleep(sleepPeriod)

	comm1Snapshot := comm1Reporter.GetStateSnapshot()
	comm2Snapshot := comm2Reporter.GetStateSnapshot()
	require.NotEmpty(t, comm1Snapshot.Alias)
	require.NotEmpty(t, comm1Snapshot.Alias)
	require.NotEmpty(t, c1Data.Alias)
	require.NotEmpty(t, c2Data.Alias)
	require.Equal(t, 4, len(comm1Snapshot.Peers)+len(comm2Snapshot.Peers))

	printTitle("Aliases")
	log.Info().Msgf("commserver1 alias is %d", comm1Snapshot.Alias)
	log.Info().Msgf("commserver2 alias is %d", comm2Snapshot.Alias)
	log.Info().Msgf("client1 alias is %d", c1Data.Alias)
	log.Info().Msgf("client2 alias is %d", c2Data.Alias)

	printTitle("Connections")
	fmt.Println(comm1Snapshot.Peers)
	fmt.Println(comm2Snapshot.Peers)

	printTitle("Authorizing clients")

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
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	comm2Snapshot = comm2Reporter.GetStateSnapshot()

	printTitle("Both clients are subscribing to 'test' topic")
	require.NoError(t, c1.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))

	// NOTE: wait until subscriptions are ready
	time.Sleep(sleepPeriod)
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	comm2Snapshot = comm2Reporter.GetStateSnapshot()
	// require.True(t, comm1Snapshot.Peers[c1Data.Alias].Topics["test"])
	// require.True(t, comm2Snapshot.Peers[c2Data.Alias].Topics["test"])

	printTitle("Each client sends a topic message, by reliable channel")
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

	printTitle("Each client sends a topic message, by unreliable channel")
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

	printTitle("Remove topic")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{}))

	time.Sleep(sleepPeriod)
	comm2Snapshot = comm2Reporter.GetStateSnapshot()
	// require.False(t, comm2Snapshot.Peers[c2Data.Alias].Topics["test"])

	printTitle("Testing webrtc connection close")
	c2.StopReliableQueue <- true
	c2.StopUnreliableQueue <- true
	go c2.conn.Close()
	c2.conn = nil
	c2.Connect(c2Data.Alias, comm1Snapshot.Alias)
	c2.authMessage <- authBytes
	time.Sleep(longSleepPeriod)

	printTitle("Subscribe to topics again")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	time.Sleep(longSleepPeriod)
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	// require.True(t, comm1Snapshot.Peers[c1Data.Alias].Topics["test"])
	// require.True(t, comm1Snapshot.Peers[c2Data.Alias].Topics["test"])

	printTitle("Each client sends a topic message, by reliable channel")
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
