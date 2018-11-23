package worldcomm

import (
	"errors"
	"github.com/decentraland/communications-server-go/agent"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

var appName = "world-comm-test"

type MockWebsocket struct {
	messages [][]byte
	index    int
}

func (ws *MockWebsocket) SetWriteDeadline(t time.Time) error          { return nil }
func (ws *MockWebsocket) SetReadDeadline(t time.Time) error           { return nil }
func (ws *MockWebsocket) SetReadLimit(int64)                          {}
func (ws *MockWebsocket) SetPongHandler(h func(appData string) error) {}
func (ws *MockWebsocket) ReadMessage() (p []byte, err error) {
	if ws.index < len(ws.messages) {
		i := ws.index
		ws.index += 1
		return ws.messages[i], nil
	}

	return nil, errors.New("closed")
}
func (ws *MockWebsocket) WriteMessage(bytes []byte) error { return nil }
func (ws *MockWebsocket) WriteCloseMessage() error        { return nil }
func (ws *MockWebsocket) WritePingMessage() error         { return nil }
func (ws *MockWebsocket) Close() error                    { return nil }

func makeClientPosition(x float32, z float32) *clientPosition {
	p := parcel{
		x: int32(x),
		z: int32(z),
	}

	q := [7]float32{x, 0, z, 0, 0, 0, 0}

	return &clientPosition{parcel: p, quaternion: q, lastUpdate: time.Now().Add(-10 * time.Hour)}
}

func setupClient(state *worldCommunicationState) (*client, *MockWebsocket) {
	conn := &MockWebsocket{}
	c := makeClient(conn)
	c.position = makeClientPosition(0, 0)
	state.clients[c] = true
	return c, conn
}

func setupOpenFlowClient(state *worldCommunicationState) (*client, *MockWebsocket) {
	c, conn := setupClient(state)
	c.flowStatus = FlowStatus_OPEN
	return c, conn
}

func TestReadToEnqueueMessage(t *testing.T) {
	_, ms := now()
	msg := &PositionMessage{
		Type:      MessageType_POSITION,
		Time:      ms,
		PositionX: 0,
		PositionY: 0,
		PositionZ: 0,
		RotationX: 0,
		RotationY: 0,
		RotationZ: 0,
		RotationW: 0,
		Alias:     0,
	}
	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	msgs := [][]byte{data}
	conn := &MockWebsocket{messages: msgs}

	agent, err := agent.Make(appName, "")
	require.NoError(t, err)

	state := makeState(agent)
	defer closeState(&state)
	c := makeClient(conn)
	defer close(c.send)

	go read(&state, c)

	in := <-state.positionQueue
	require.Equal(t, c, in.c, "message is not properly enqueued")
	require.Equal(t, data, in.bytes, "message is not properly enqueued")

	clientToUnregister := <-state.unregister
	require.Equal(t, c, clientToUnregister, "after close, client is not enqueued for unregistration")
}

func TestProcessMessageToChangeFlowStatus(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)

	state := makeState(agent)
	c := makeClient(nil)
	defer close(c.send)

	ts, ms := now()

	msg := &FlowStatusMessage{Type: MessageType_FLOW_STATUS, Time: ms, FlowStatus: FlowStatus_OPEN}
	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	in := &inMessage{c: c, bytes: data, tMsg: ts}

	go func() {
		state.flowStatusQueue <- in
	}()

	processQueues(&state)
	require.Equal(t, c.flowStatus, FlowStatus_OPEN)
	closeState(&state)
}

func TestProcessMessageToBroadcastChat(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := makeState(agent)
	defer closeState(&state)

	c1, _ := setupClient(&state)
	defer close(c1.send)

	c2, _ := setupOpenFlowClient(&state)
	defer close(c2.send)

	ts, ms := now()
	msg := &ChatMessage{
		Type:      MessageType_CHAT,
		Time:      ms,
		MessageId: "1",
		PositionX: 0,
		PositionZ: 0,
		Text:      "text",
		Alias:     0,
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	in := &inMessage{c: c1, bytes: data, tMsg: ts}
	go func() {
		state.chatQueue <- in
	}()

	processQueues(&state)
	require.Equal(t, len(c1.send), 0, "data was sent")
	require.Equal(t, len(c2.send), 1, "no data was sent")
}

func TestProcessMessageToBroadcastProfile(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := makeState(agent)
	defer closeState(&state)

	c1, _ := setupClient(&state)
	defer close(c1.send)

	c2, _ := setupOpenFlowClient(&state)
	defer close(c2.send)

	ts, ms := now()
	msg := &ProfileMessage{
		Type:        MessageType_PROFILE,
		Time:        ms,
		PositionX:   0,
		PositionZ:   0,
		AvatarType:  "fox",
		DisplayName: "a fox",
		PeerId:      "peer",
		PublicKey:   "pubkey",
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	in := &inMessage{c: c1, bytes: data, tMsg: ts}
	go func() {
		state.profileQueue <- in
	}()

	processQueues(&state)

	require.Equal(t, len(c1.send), 0, "data was sent")
	require.Equal(t, len(c2.send), 1, "no data was sent")
}

func TestProcessMessageToSaveAndBroadcastPosition(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := makeState(agent)
	defer closeState(&state)

	c1, _ := setupClient(&state)
	defer close(c1.send)

	c2, _ := setupOpenFlowClient(&state)
	defer close(c2.send)

	ts, ms := now()
	msg := &PositionMessage{
		Type:      MessageType_POSITION,
		Time:      ms,
		PositionX: 10.3,
		PositionY: 0,
		PositionZ: 9,
		RotationX: 0,
		RotationY: 0,
		RotationZ: 0,
		RotationW: 0,
		Alias:     0,
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	in := &inMessage{c: c1, bytes: data, tMsg: ts}
	go func() {
		state.positionQueue <- in
	}()

	processQueues(&state)

	require.Equal(t, len(c1.send), 0, "data was sent")
	require.Equal(t, len(c2.send), 1, "no data was sent")

	require.Equal(t, c1.position.quaternion, [7]float32{10.3, 0, 9, 0, 0, 0, 0}, "position is not stored")
	require.Equal(t, c1.position.parcel, parcel{10, 9}, "parcel is not stored")
}

func TestBroadcastOutsideCommArea(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := makeState(agent)
	defer closeState(&state)

	c1, _ := setupClient(&state)
	defer close(c1.send)

	c2, _ := setupOpenFlowClient(&state)
	c2.position = makeClientPosition(20, 0)
	defer close(c2.send)

	ts, ms := now()
	msg := &ChatMessage{
		Type:      MessageType_CHAT,
		Time:      ms,
		MessageId: "1",
		PositionX: 0,
		PositionZ: 0,
		Text:      "text",
		Alias:     0,
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	in := &inMessage{c: c1, bytes: data, tMsg: ts}
	go func() {
		state.chatQueue <- in
	}()

	processQueues(&state)

	require.Equal(t, len(c1.send), 0, "data was sent")
	require.Equal(t, len(c2.send), 0, "data was sent")
}

func TestBroadcastClosedFlow(t *testing.T) {
	agent, err := agent.Make(appName, "")
	require.NoError(t, err)
	state := makeState(agent)
	defer closeState(&state)

	c1, _ := setupClient(&state)
	defer close(c1.send)

	c2, _ := setupClient(&state)
	defer close(c2.send)

	ts, ms := now()
	msg := &ChatMessage{
		Type:      MessageType_CHAT,
		Time:      ms,
		MessageId: "1",
		PositionX: 0,
		PositionZ: 0,
		Text:      "text",
		Alias:     0,
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	in := &inMessage{c: c1, bytes: data, tMsg: ts}
	go func() {
		state.chatQueue <- in
	}()

	processQueues(&state)

	require.Equal(t, len(c1.send), 0, "data was sent")
	require.Equal(t, len(c2.send), 0, "data was sent")
}

func BenchmarkProcessPositionMessage(b *testing.B) {
	agent, err := agent.Make(appName, "")
	require.NoError(b, err)
	state := makeState(agent)
	defer closeState(&state)

	c1, _ := setupClient(&state)
	defer close(c1.send)
	state.clients[c1] = true

	for i := 0; i < 100; i += 1 {
		c, _ := setupOpenFlowClient(&state)
		defer close(c.send)
		state.clients[c] = true

		go func() {
			for {
				select {
				case _, ok := <-c.send:
					if !ok {
						return
					}

					n := len(c.send)
					for i := 0; i < n; i++ {
						_ = <-c.send
					}
				}
			}
		}()
	}

	ts, ms := now()
	msg := &PositionMessage{
		Type:      MessageType_POSITION,
		Time:      ms,
		PositionX: 10.3,
		PositionY: 0,
		PositionZ: 9,
		RotationX: 0,
		RotationY: 0,
		RotationZ: 0,
		RotationW: 0,
		Alias:     0,
	}

	data, err := proto.Marshal(msg)
	require.NoError(b, err)

	in := &inMessage{c: c1, bytes: data, tMsg: ts}

	b.Run("processMessage loop", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			go func() {
				state.positionQueue <- in
			}()
			processQueues(&state)
		}
	})
}
