package worldcomm

import (
	"errors"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type MockWriter struct {
}

type MockWebsocket struct {
	lastWrite []byte
	messages  [][]byte
	index     int
}

func (ws *MockWebsocket) ReadMessage() (messageType int, p []byte, err error) {
	if ws.index < len(ws.messages) {
		i := ws.index
		ws.index += 1
		return websocket.BinaryMessage, ws.messages[i], nil
	}

	return websocket.BinaryMessage, nil, errors.New("closed")
}

func (ws *MockWebsocket) Close() error {
	return nil
}

func (ws *MockWebsocket) WriteMessage(messageType int, bytes []byte) error {
	ws.lastWrite = bytes
	return nil
}

func now() (time.Time, float64) {
	now := time.Now()
	return now, float64(now.UnixNano() / int64(time.Millisecond))
}

func makeClientPosition(x float32, z float32) *clientPosition {
	p := parcel{
		x: int32(x),
		z: int32(z),
	}

	q := [7]float32{x, 0, z, 0, 0, 0, 0}

	return &clientPosition{parcel: p, quaternion: q, lastUpdate: time.Now().Add(-10 * time.Hour)}
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
		PeerId:    "peer",
	}
	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	msgs := [][]byte{data}
	conn := &MockWebsocket{messages: msgs}

	state := MakeState()
	defer Close(state)
	c := &client{conn: conn, position: nil, flowStatus: FlowStatus_UNKNOWN_STATUS}

	go read(state, c)

	enqueuedMessage := <-state.queue
	require.Equal(t, c, enqueuedMessage.client, "message is not properly enqueued")
	require.Equal(t, data, enqueuedMessage.bytes, "message is not properly enqueued")

	clientToUnregister := <-state.unregister
	require.Equal(t, c, clientToUnregister, "after close, client is not enqueued for unregistration")
}

func TestProcessMessageToChangeFlowStatus(t *testing.T) {
	state := MakeState()
	defer Close(state)
	c := &client{conn: nil, position: nil, flowStatus: FlowStatus_UNKNOWN_STATUS}

	ts, ms := now()

	msg := &FlowStatusMessage{Type: MessageType_FLOW_STATUS, Time: ms, FlowStatus: FlowStatus_OPEN}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)
	enqueuedMessage := enqueuedMessage{client: c, bytes: data, ts: ts, msgType: MessageType_FLOW_STATUS}

	processMessage(state, &enqueuedMessage)

	require.Equal(t, c.flowStatus, FlowStatus_OPEN)
}

func TestProcessMessageToBroadcastChat(t *testing.T) {
	conn1 := &MockWebsocket{}
	c1 := &client{conn: conn1, position: makeClientPosition(0, 0)}

	conn2 := &MockWebsocket{}
	c2 := &client{conn: conn2, position: makeClientPosition(0, 0)}

	state := MakeState()
	defer Close(state)
	state.clients[c1] = true
	state.clients[c2] = true

	ts, ms := now()
	msg := &ChatMessage{
		Type:      MessageType_CHAT,
		Time:      ms,
		MessageId: "1",
		PositionX: 0,
		PositionZ: 0,
		Text:      "text",
		PeerId:    "peer",
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)
	enqueuedMessage := enqueuedMessage{client: c1, bytes: data, msgType: MessageType_CHAT, ts: ts}

	processMessage(state, &enqueuedMessage)

	require.Equal(t, conn2.lastWrite, data, "not data was sent")
}

func TestProcessMessageToBroadcastProfile(t *testing.T) {
	conn1 := &MockWebsocket{}
	c1 := &client{conn: conn1, position: makeClientPosition(0, 0)}

	conn2 := &MockWebsocket{}
	c2 := &client{conn: conn2, position: makeClientPosition(0, 0)}

	state := MakeState()
	defer Close(state)
	state.clients[c1] = true
	state.clients[c2] = true

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
	enqueuedMessage := enqueuedMessage{client: c1, bytes: data, msgType: MessageType_PROFILE, ts: ts}

	processMessage(state, &enqueuedMessage)

	require.Equal(t, conn2.lastWrite, data, "not data was sent")
}

func TestProcessMessageToSaveAndBroadcastPosition(t *testing.T) {
	conn1 := &MockWebsocket{}
	c1 := &client{conn: conn1, position: makeClientPosition(0, 0)}

	conn2 := &MockWebsocket{}
	c2 := &client{conn: conn2, position: makeClientPosition(0, 0)}

	state := MakeState()
	defer Close(state)
	state.clients[c1] = true
	state.clients[c2] = true

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
		PeerId:    "peer",
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)
	enqueuedMessage := enqueuedMessage{client: c1, bytes: data, msgType: MessageType_POSITION, ts: ts}

	processMessage(state, &enqueuedMessage)

	require.Equal(t, conn2.lastWrite, data, "not data was sent")

	require.Equal(t, c1.position.quaternion, [7]float32{10.3, 0, 9, 0, 0, 0, 0}, "position is not stored")
	require.Equal(t, c1.position.parcel, parcel{10, 9}, "parcel is not stored")
}

func TestBroadcastOutsideCommArea(t *testing.T) {
	conn1 := &MockWebsocket{}
	c1 := &client{conn: conn1, position: makeClientPosition(0, 0)}

	conn2 := &MockWebsocket{}
	c2 := &client{conn: conn2, position: makeClientPosition(11, 0)}

	state := MakeState()
	defer Close(state)
	state.clients[c1] = true
	state.clients[c2] = true

	ts, ms := now()
	msg := &ChatMessage{
		Type:      MessageType_CHAT,
		Time:      ms,
		MessageId: "1",
		PositionX: 0,
		PositionZ: 0,
		Text:      "text",
		PeerId:    "peer",
	}

	data, err := proto.Marshal(msg)
	require.NoError(t, err)
	enqueuedMessage := enqueuedMessage{client: c1, bytes: data, msgType: MessageType_CHAT, ts: ts}

	processMessage(state, &enqueuedMessage)

	require.Equal(t, len(conn2.lastWrite), 0, "data was sent")
}

func BenchmarkProcessPositionMessage(b *testing.B) {
	conn1 := &MockWebsocket{}
	c1 := &client{conn: conn1, position: makeClientPosition(0, 0)}

	conn2 := &MockWebsocket{}
	c2 := &client{conn: conn2, position: makeClientPosition(0, 0)}

	state := MakeState()
	defer Close(state)
	state.clients[c1] = true
	state.clients[c2] = true

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
		PeerId:    "peer",
	}

	data, err := proto.Marshal(msg)
	require.NoError(b, err)
	enqueuedMessage := enqueuedMessage{client: c1, bytes: data, msgType: MessageType_POSITION, ts: ts}
	for i := 0; i < b.N; i++ {
		processMessage(state, &enqueuedMessage)
	}
}
