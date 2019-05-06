// +build integration

package simulation

import (
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/decentraland/webrtc-broker/pkg/authentication"

	_testing "github.com/decentraland/webrtc-broker/internal/testing"
	"github.com/decentraland/webrtc-broker/pkg/commserver"
	"github.com/decentraland/webrtc-broker/pkg/coordinator"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type MockAuthenticator = _testing.MockAuthenticator

var appName = "e2e-test"

const (
	sleepPeriod     = 5 * time.Second
	longSleepPeriod = 15 * time.Second
)

type MockServerSelector struct {
	serverAliases []uint64
}

func (r *MockServerSelector) ServerRegistered(server *coordinator.Peer) {
	r.serverAliases = append(r.serverAliases, server.Alias)
}

func (r *MockServerSelector) ServerUnregistered(server *coordinator.Peer) {}

func (r *MockServerSelector) GetServerAliasList(forPeer *coordinator.Peer) []uint64 {
	peers := []uint64{}

	for _, alias := range r.serverAliases {
		peers = append(peers, alias)
	}

	return peers
}

func makeTestAuthenticator() *MockAuthenticator {
	auth := _testing.MakeWithAuthResponse(true)
	auth.GenerateAuthURL_ = func(baseURL string, role protocol.Role) (string, error) {
		return fmt.Sprintf("%s?method=testAuth", baseURL), nil
	}
	auth.GenerateAuthMessage_ = func(role protocol.Role) (*protocol.AuthMessage, error) {
		return &protocol.AuthMessage{
			Type:   protocol.MessageType_AUTH,
			Role:   role,
			Method: "testAuth",
		}, nil
	}

	return auth
}

func printTitle(title string) {
	s := fmt.Sprintf("=== %s ===", title)
	log.Println(s)
}

func startCoordinator(t *testing.T) (*coordinator.CoordinatorState, *http.Server, string, string) {
	host := "localhost"
	port := 9999
	addr := fmt.Sprintf("%s:%d", host, port)

	log := logrus.New()
	auth := authentication.Make()
	auth.AddOrUpdateAuthenticator("testAuth", makeTestAuthenticator())
	config := coordinator.Config{
		ServerSelector: &MockServerSelector{serverAliases: []uint64{}},
		Auth:           auth,
		Log:            log,
	}
	state := coordinator.MakeState(&config)

	go coordinator.Process(state)

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

	s := &http.Server{Addr: addr, Handler: mux}
	go func() {
		t.Log("starting coordinator")
		s.ListenAndServe()
	}()

	discoveryURL := fmt.Sprintf("ws://%s/discover", addr)
	connectURL := fmt.Sprintf("ws://%s/connect", addr)
	return state, s, discoveryURL, connectURL
}

type peerSnapshot struct {
	Topics map[string]bool
}

type commServerSnapshot struct {
	Alias      uint64
	PeersCount int
	Peers      map[uint64]peerSnapshot
}

type testReporter struct {
	RequestData chan bool
	Data        chan commServerSnapshot
}

func (r *testReporter) Report(state *commserver.State) {
	select {
	case <-r.RequestData:
		peers := make(map[uint64]peerSnapshot)

		for _, p := range state.Peers {
			s := peerSnapshot{
				Topics: make(map[string]bool),
			}

			for topic := range p.Topics {
				s.Topics[topic] = true
			}

			peers[p.Alias] = s
		}

		snapshot := commServerSnapshot{
			Alias:      state.Alias,
			PeersCount: len(state.Peers),
			Peers:      peers,
		}
		r.Data <- snapshot
	default:
	}
}

func (r *testReporter) GetStateSnapshot() commServerSnapshot {
	r.RequestData <- true
	return <-r.Data
}

func startCommServer(t *testing.T, discoveryURL string) *testReporter {
	logger := logrus.New()
	auth := authentication.Make()
	authenticator := makeTestAuthenticator()
	auth.AddOrUpdateAuthenticator("testAuth", authenticator)

	reporter := &testReporter{
		RequestData: make(chan bool),
		Data:        make(chan commServerSnapshot),
	}

	config := commserver.Config{
		Auth:           auth,
		Log:            logger,
		AuthMethod:     "testAuth",
		CoordinatorURL: discoveryURL,
		ReportPeriod:   1 * time.Second,
		Reporter:       func(state *commserver.State) { reporter.Report(state) },
	}

	ws, err := commserver.MakeState(&config)
	require.NoError(t, err)
	t.Log("starting communication server node", discoveryURL)

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
	_, server, discoveryURL, connectURL := startCoordinator(t)
	defer server.Close()

	dataMessage := protocol.DataMessage{}

	printTitle("starting comm servers")
	comm1Reporter := startCommServer(t, discoveryURL)
	comm2Reporter := startCommServer(t, discoveryURL)

	auth := authentication.Make()
	authenticator := makeTestAuthenticator()
	auth.AddOrUpdateAuthenticator("testAuth", authenticator)

	c1ReceivedReliable := make(chan recvMessage, 256)
	c1ReceivedUnreliable := make(chan recvMessage, 256)
	config := Config{
		Auth:           auth,
		AuthMethod:     "testAuth",
		CoordinatorURL: connectURL,
		OnMessageReceived: func(reliable bool, msgType protocol.MessageType, raw []byte) {
			m := recvMessage{msgType: msgType, raw: raw}
			if reliable {
				c1ReceivedReliable <- m
			} else {
				c1ReceivedUnreliable <- m
			}
		},
	}
	c1 := MakeClient(&config)

	c2ReceivedReliable := make(chan recvMessage, 256)
	c2ReceivedUnreliable := make(chan recvMessage, 256)
	config = Config{
		Auth:           auth,
		AuthMethod:     "testAuth",
		CoordinatorURL: connectURL,
		OnMessageReceived: func(reliable bool, msgType protocol.MessageType, raw []byte) {
			m := recvMessage{msgType: msgType, raw: raw}
			if reliable {
				c2ReceivedReliable <- m
			} else {
				c2ReceivedUnreliable <- m
			}
		},
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
	require.Equal(t, 4, comm1Snapshot.PeersCount+comm2Snapshot.PeersCount)

	printTitle("Aliases")
	log.Println("commserver1 alias is", comm1Snapshot.Alias)
	log.Println("commserver2 alias is", comm2Snapshot.Alias)
	log.Println("client1 alias is", c1Data.Alias)
	log.Println("client2 alias is", c2Data.Alias)

	printTitle("Connections")
	log.Println(comm1Snapshot.Peers)
	log.Println(comm2Snapshot.Peers)

	printTitle("Authorizing clients")

	authMessage := protocol.AuthMessage{
		Type:   protocol.MessageType_AUTH,
		Method: "testAuth",
		Role:   protocol.Role_CLIENT,
	}
	authBytes, err := proto.Marshal(&authMessage)
	require.NoError(t, err)

	c1.authMessage <- authBytes
	c2.authMessage <- authBytes

	recvMsg := <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_AUTH, recvMsg.msgType)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_AUTH, recvMsg.msgType)

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
	require.True(t, comm1Snapshot.Peers[c1Data.Alias].Topics["test"])
	require.True(t, comm2Snapshot.Peers[c2Data.Alias].Topics["test"])

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

	recvMsg = <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &dataMessage))
	require.Equal(t, []byte("c2 test"), dataMessage.Body)
	require.Equal(t, c2Data.Alias, dataMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &dataMessage))
	require.Equal(t, []byte("c1 test"), dataMessage.Body)
	require.Equal(t, c1Data.Alias, dataMessage.FromAlias)

	printTitle("Each client sends a topic message, by unreliable channel")
	c1.SendUnreliable <- c1EncodedMessage
	c2.SendUnreliable <- c2EncodedMessage

	time.Sleep(longSleepPeriod)
	require.Len(t, c1ReceivedUnreliable, 1)
	require.Len(t, c2ReceivedUnreliable, 1)

	recvMsg = <-c1ReceivedUnreliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &dataMessage))
	require.Equal(t, []byte("c2 test"), dataMessage.Body)
	require.Equal(t, c2Data.Alias, dataMessage.FromAlias)

	recvMsg = <-c2ReceivedUnreliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &dataMessage))
	require.Equal(t, []byte("c1 test"), dataMessage.Body)
	require.Equal(t, c1Data.Alias, dataMessage.FromAlias)

	printTitle("Remove topic")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{}))

	time.Sleep(sleepPeriod)
	comm2Snapshot = comm2Reporter.GetStateSnapshot()
	require.False(t, comm2Snapshot.Peers[c2Data.Alias].Topics["test"])

	printTitle("Testing webrtc connection close")
	c2.StopReliableQueue <- true
	c2.StopUnreliableQueue <- true
	go c2.conn.Close()
	c2.conn = nil
	c2.Connect(c2Data.Alias, comm1Snapshot.Alias)

	c2.authMessage <- authBytes
	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_AUTH, recvMsg.msgType)

	printTitle("Subscribe to topics again")
	require.NoError(t, c2.SendTopicSubscriptionMessage(map[string]bool{"test": true}))
	time.Sleep(longSleepPeriod)
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	require.True(t, comm1Snapshot.Peers[c1Data.Alias].Topics["test"])
	require.True(t, comm1Snapshot.Peers[c2Data.Alias].Topics["test"])

	printTitle("Each client sends a topic message, by reliable channel")
	c1.SendReliable <- c1EncodedMessage
	c2.SendReliable <- c2EncodedMessage

	time.Sleep(sleepPeriod)
	require.Len(t, c1ReceivedReliable, 1)
	require.Len(t, c2ReceivedReliable, 1)

	recvMsg = <-c1ReceivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &dataMessage))
	require.Equal(t, []byte("c2 test"), dataMessage.Body)
	require.Equal(t, c2Data.Alias, dataMessage.FromAlias)

	recvMsg = <-c2ReceivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.msgType)
	require.NoError(t, proto.Unmarshal(recvMsg.raw, &dataMessage))
	require.Equal(t, []byte("c1 test"), dataMessage.Body)
	require.Equal(t, c1Data.Alias, dataMessage.FromAlias)

	log.Println("TEST END")
}
