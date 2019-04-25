// +build integration

package simulation

import (
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/webrtc"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/coordinator"
	_testing "github.com/decentraland/communications-server-go/internal/testing"
	"github.com/decentraland/communications-server-go/internal/utils"
	"github.com/decentraland/communications-server-go/internal/worldcomm"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"
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
	auth.GenerateAuthURL_ = func(baseUrl string, role protocol.Role) (string, error) {
		return fmt.Sprintf("%s?method=testAuth", baseUrl), nil
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

func startCoordinator(t *testing.T) (coordinator.CoordinatorState, *http.Server, string, string) {
	host := "localhost"
	port := 9999
	addr := fmt.Sprintf("%s:%d", host, port)

	agent, err := agent.Make(appName, "")
	require.NoError(t, err)

	selector := &MockServerSelector{
		serverAliases: []uint64{},
	}
	cs := coordinator.MakeState(agent, selector)

	auth := makeTestAuthenticator()
	cs.Auth.AddOrUpdateAuthenticator("testAuth", auth)
	go coordinator.Process(&cs)

	mux := http.NewServeMux()
	mux.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeDiscoverRequest(&cs, w, r)
		require.NoError(t, err)
		coordinator.ConnectCommServer(&cs, ws)
	})

	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeConnectRequest(&cs, w, r)

		require.NoError(t, err)
		coordinator.ConnectClient(&cs, ws)
	})

	s := &http.Server{Addr: addr, Handler: mux}
	go func() {
		t.Log("starting coordinator")
		s.ListenAndServe()
	}()

	discoveryUrl := fmt.Sprintf("ws://%s/discover", addr)
	connectUrl := fmt.Sprintf("ws://%s/connect", addr)
	return cs, s, discoveryUrl, connectUrl
}

type peerSnapshot struct {
	Topics map[string]bool
}

type worldCommSnapshot struct {
	Alias      uint64
	PeersCount int
	Peers      map[uint64]peerSnapshot
}

type testReporter struct {
	RequestData chan bool
	Data        chan worldCommSnapshot
}

func (r *testReporter) Report(state *worldcomm.WorldCommunicationState) {
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

		snapshot := worldCommSnapshot{
			Alias:      state.Alias,
			PeersCount: len(state.Peers),
			Peers:      peers,
		}
		r.Data <- snapshot
	default:
	}
}

func (r *testReporter) GetStateSnapshot() worldCommSnapshot {
	r.RequestData <- true
	return <-r.Data
}

func startCommServer(t *testing.T, discoveryUrl string) *testReporter {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{})
	services := worldcomm.Services{
		Auth:       authentication.Make(),
		Marshaller: &protocol.Marshaller{},
		Log:        logger,
		WebRtc:     webrtc.MakeWebRtc(),
		Agent:      &worldcomm.WorldCommAgent{Agent: agent},
		Zipper:     &utils.GzipCompression{},
	}

	authenticator := makeTestAuthenticator()
	services.Auth.AddOrUpdateAuthenticator("testAuth", authenticator)

	reporter := &testReporter{
		RequestData: make(chan bool),
		Data:        make(chan worldCommSnapshot),
	}

	config := worldcomm.Config{
		AuthMethod:     "testAuth",
		CoordinatorUrl: discoveryUrl,
		Services:       services,
		ReportPeriod:   1 * time.Second,
		Reporter:       reporter,
	}

	ws := worldcomm.MakeState(config)
	t.Log("starting communication server node", discoveryUrl)

	require.NoError(t, worldcomm.ConnectCoordinator(&ws))
	go worldcomm.ProcessMessagesQueue(&ws)
	go worldcomm.Process(&ws)
	return reporter
}

type TestClient struct {
	client *Client
	alias  uint64
	avatar string
}

func makeTestClient(id string, connectUrl string) *TestClient {
	url := fmt.Sprintf("%s?method=testAuth", connectUrl)
	client := MakeClient(id, url)

	client.receivedReliable = make(chan ReceivedMessage, 256)
	client.receivedUnreliable = make(chan ReceivedMessage, 256)
	return &TestClient{client: client, avatar: getRandomAvatar()}
}

func (tc *TestClient) start(t *testing.T) WorldData {
	go func() {
		require.NoError(t, tc.client.startCoordination())
	}()

	worldData := <-tc.client.worldData
	tc.alias = worldData.MyAlias

	return worldData
}

func (tc *TestClient) sendTopicSubscriptionMessage(t *testing.T, topics map[string]bool) {
	require.NoError(t, tc.client.sendTopicSubscriptionMessage(topics))
}

func (tc *TestClient) encodeProfileMessage(t *testing.T, topic string) []byte {
	ms := utils.NowMs()
	bytes, err := encodeTopicMessage(topic, &protocol.ProfileData{
		Category:    protocol.Category_PROFILE,
		Time:        ms,
		AvatarType:  tc.avatar,
		DisplayName: fmt.Sprintf("%d", tc.alias),
		PublicKey:   "key",
	})

	require.NoError(t, err)

	return bytes
}

func (tc *TestClient) sendProfileUnreliableMessage(t *testing.T, topic string) {
	tc.client.sendUnreliable <- tc.encodeProfileMessage(t, topic)
}

func (tc *TestClient) sendProfileReliableMessage(t *testing.T, topic string) {
	tc.client.sendReliable <- tc.encodeProfileMessage(t, topic)
}

func TestE2E(t *testing.T) {
	_, server, discoveryUrl, connectUrl := startCoordinator(t)
	defer server.Close()

	dataMessage := protocol.DataMessage{}
	profileData := protocol.ProfileData{}

	printTitle("starting comm servers")
	comm1Reporter := startCommServer(t, discoveryUrl)
	comm2Reporter := startCommServer(t, discoveryUrl)

	c1 := makeTestClient("client1", connectUrl)
	c2 := makeTestClient("client2", connectUrl)

	printTitle("Starting client1")
	client1WorldData := c1.start(t)
	require.NoError(t, c1.client.connect(client1WorldData.MyAlias, client1WorldData.AvailableServers[0]))

	printTitle("Starting client2")
	client2WorldData := c2.start(t)
	require.NoError(t, c2.client.connect(client1WorldData.MyAlias, client2WorldData.AvailableServers[1]))

	// NOTE: wait until connections are ready
	time.Sleep(sleepPeriod)

	comm1Snapshot := comm1Reporter.GetStateSnapshot()
	comm2Snapshot := comm2Reporter.GetStateSnapshot()
	require.NotEmpty(t, comm1Snapshot.Alias)
	require.NotEmpty(t, comm1Snapshot.Alias)
	require.NotEmpty(t, c1.alias)
	require.NotEmpty(t, c2.alias)
	require.Equal(t, 4, comm1Snapshot.PeersCount+comm2Snapshot.PeersCount)

	printTitle("Aliases")
	log.Println("commserver1 alias is", comm1Snapshot.Alias)
	log.Println("commserver2 alias is", comm2Snapshot.Alias)
	log.Println("client1 alias is", c1.alias)
	log.Println("client2 alias is", c2.alias)

	printTitle("Connections")
	log.Println(comm1Snapshot.Peers)
	log.Println(comm2Snapshot.Peers)

	printTitle("Authorizing clients")
	authBytes, err := encodeAuthMessage("testAuth", protocol.Role_CLIENT, nil)
	require.NoError(t, err)
	c1.client.authMessage <- authBytes
	c2.client.authMessage <- authBytes

	recvMsg := <-c1.client.receivedReliable
	require.Equal(t, protocol.MessageType_AUTH, recvMsg.Type)

	recvMsg = <-c2.client.receivedReliable
	require.Equal(t, protocol.MessageType_AUTH, recvMsg.Type)

	// NOTE: wait until connections are authenticated
	time.Sleep(longSleepPeriod)
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	comm2Snapshot = comm2Reporter.GetStateSnapshot()

	printTitle("Both clients are subscribing to 'profile' topic")
	c1.sendTopicSubscriptionMessage(t, map[string]bool{"profile": true})
	c2.sendTopicSubscriptionMessage(t, map[string]bool{"profile": true})

	// NOTE: wait until subscriptions are ready
	time.Sleep(sleepPeriod)
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	comm2Snapshot = comm2Reporter.GetStateSnapshot()
	require.True(t, comm1Snapshot.Peers[c1.alias].Topics["profile"])
	require.True(t, comm2Snapshot.Peers[c2.alias].Topics["profile"])

	printTitle("Each client sends a profile message, by reliable channel")
	c1.sendProfileReliableMessage(t, "profile")
	c2.sendProfileReliableMessage(t, "profile")

	// NOTE wait until messages are received
	time.Sleep(longSleepPeriod)
	require.Len(t, c1.client.receivedReliable, 1)
	require.Len(t, c2.client.receivedReliable, 1)

	recvMsg = <-c1.client.receivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.Type)
	require.NoError(t, proto.Unmarshal(recvMsg.RawMessage, &dataMessage))
	require.NoError(t, proto.Unmarshal(dataMessage.Body, &profileData))
	require.Equal(t, protocol.Category_PROFILE, profileData.Category)
	require.Equal(t, c2.alias, dataMessage.FromAlias)

	recvMsg = <-c2.client.receivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.Type)
	require.NoError(t, proto.Unmarshal(recvMsg.RawMessage, &dataMessage))
	require.NoError(t, proto.Unmarshal(dataMessage.Body, &profileData))
	require.Equal(t, protocol.Category_PROFILE, profileData.Category)
	require.Equal(t, c1.alias, dataMessage.FromAlias)

	printTitle("Each client sends a profile message, by unreliable channel")
	c1.sendProfileUnreliableMessage(t, "profile")
	c2.sendProfileUnreliableMessage(t, "profile")

	time.Sleep(longSleepPeriod)
	require.Len(t, c1.client.receivedUnreliable, 1)
	require.Len(t, c2.client.receivedUnreliable, 1)

	recvMsg = <-c1.client.receivedUnreliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.Type)
	require.NoError(t, proto.Unmarshal(recvMsg.RawMessage, &dataMessage))
	require.NoError(t, proto.Unmarshal(dataMessage.Body, &profileData))
	require.Equal(t, protocol.Category_PROFILE, profileData.Category)
	require.Equal(t, c2.alias, dataMessage.FromAlias)

	recvMsg = <-c2.client.receivedUnreliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.Type)
	require.NoError(t, proto.Unmarshal(recvMsg.RawMessage, &dataMessage))
	require.NoError(t, proto.Unmarshal(dataMessage.Body, &profileData))
	require.Equal(t, protocol.Category_PROFILE, profileData.Category)
	require.Equal(t, c1.alias, dataMessage.FromAlias)

	printTitle("Remove topic")
	c2.sendTopicSubscriptionMessage(t, map[string]bool{})

	time.Sleep(sleepPeriod)
	comm2Snapshot = comm2Reporter.GetStateSnapshot()
	require.False(t, comm2Snapshot.Peers[c2.alias].Topics["profile"])

	printTitle("Testing webrtc connection close")
	c2.client.stopReliableQueue <- true
	c2.client.stopUnreliableQueue <- true
	go c2.client.conn.Close()
	c2.client.conn = nil
	c2.client.connect(client2WorldData.MyAlias, comm1Snapshot.Alias)

	c2.client.authMessage <- authBytes
	recvMsg = <-c2.client.receivedReliable
	require.Equal(t, protocol.MessageType_AUTH, recvMsg.Type)

	printTitle("Subscribe to topics again")
	c2.sendTopicSubscriptionMessage(t, map[string]bool{"profile": true})
	time.Sleep(longSleepPeriod)
	comm1Snapshot = comm1Reporter.GetStateSnapshot()
	require.True(t, comm1Snapshot.Peers[c1.alias].Topics["profile"])
	require.True(t, comm1Snapshot.Peers[c2.alias].Topics["profile"])

	printTitle("Each client sends a profile message, by reliable channel")
	c1.sendProfileReliableMessage(t, "profile")
	c2.sendProfileReliableMessage(t, "profile")

	time.Sleep(sleepPeriod)
	require.Len(t, c1.client.receivedReliable, 1)
	require.Len(t, c2.client.receivedReliable, 1)

	recvMsg = <-c1.client.receivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.Type)
	require.NoError(t, proto.Unmarshal(recvMsg.RawMessage, &dataMessage))
	require.NoError(t, proto.Unmarshal(dataMessage.Body, &profileData))
	require.Equal(t, protocol.Category_PROFILE, profileData.Category)
	require.Equal(t, c2.alias, dataMessage.FromAlias)

	recvMsg = <-c2.client.receivedReliable
	require.Equal(t, protocol.MessageType_DATA, recvMsg.Type)
	require.NoError(t, proto.Unmarshal(recvMsg.RawMessage, &dataMessage))
	require.NoError(t, proto.Unmarshal(dataMessage.Body, &profileData))
	require.Equal(t, protocol.Category_PROFILE, profileData.Category)
	require.Equal(t, c1.alias, dataMessage.FromAlias)

	log.Println("TEST END")
}
