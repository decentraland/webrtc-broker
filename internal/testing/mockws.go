package testing

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/decentraland/webrtc-broker/internal/ws"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/mock"
)

// MokcUpgrader mocks socket upgrade interface
type MockUpgrader struct {
	mock.Mock
}

func (m *MockUpgrader) Upgrade(w http.ResponseWriter, r *http.Request) (ws.IWebsocket, error) {
	args := m.Called(w, r)
	return args.Get(0).(ws.IWebsocket), args.Error(1)
}

type MockWebsocket struct {
	OnWrite  func([]byte)
	messages [][]byte
	index    int

	SetWriteDeadline_  func(ws *MockWebsocket, t time.Time) error
	SetReadDeadline_   func(ws *MockWebsocket, t time.Time) error
	SetReadLimit_      func(ws *MockWebsocket, limit int64)
	SetPongHandler_    func(ws *MockWebsocket, h func(appData string) error)
	ReadMessage_       func(ws *MockWebsocket) (bytes []byte, err error)
	WriteMessage_      func(ws *MockWebsocket, bytes []byte) error
	WriteCloseMessage_ func(ws *MockWebsocket) error
	WritePingMessage_  func(ws *MockWebsocket) error
	Close_             func(ws *MockWebsocket) error
}

func MakeMockWebsocket() *MockWebsocket {
	return &MockWebsocket{
		SetWriteDeadline_: func(ws *MockWebsocket, t time.Time) error { return nil },
		SetReadDeadline_:  func(ws *MockWebsocket, t time.Time) error { return nil },
		SetReadLimit_:     func(ws *MockWebsocket, limit int64) {},
		SetPongHandler_:   func(ws *MockWebsocket, h func(appData string) error) {},
		ReadMessage_: func(ws *MockWebsocket) (bytes []byte, err error) {
			if ws.index < len(ws.messages) {
				i := ws.index
				ws.index += 1
				return ws.messages[i], nil
			}

			if ws.index == 0 {
				// NOTE: this means nothing was prepared to ready, so let's block forever
				log.Println("blocking read message forever")
				x := make(chan bool)
				x <- true
			}
			return nil, errors.New("closed")
		},
		WriteMessage_: func(ws *MockWebsocket, bytes []byte) error {
			ws.OnWrite(bytes)
			return nil
		},
		WriteCloseMessage_: func(ws *MockWebsocket) error { return nil },
		WritePingMessage_:  func(ws *MockWebsocket) error { return nil },
		Close_:             func(ws *MockWebsocket) error { return nil },
	}
}

func (ws *MockWebsocket) PrepareToRead(msg proto.Message) error {
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	ws.messages = append(ws.messages, bytes)
	return nil
}

func (ws *MockWebsocket) SetWriteDeadline(t time.Time) error          { return ws.SetWriteDeadline_(ws, t) }
func (ws *MockWebsocket) SetReadDeadline(t time.Time) error           { return ws.SetReadDeadline_(ws, t) }
func (ws *MockWebsocket) SetReadLimit(l int64)                        { ws.SetReadLimit_(ws, l) }
func (ws *MockWebsocket) SetPongHandler(h func(appData string) error) { ws.SetPongHandler_(ws, h) }
func (ws *MockWebsocket) ReadMessage() (bytes []byte, err error)      { return ws.ReadMessage_(ws) }
func (ws *MockWebsocket) WriteMessage(bytes []byte) error             { return ws.WriteMessage_(ws, bytes) }
func (ws *MockWebsocket) WriteCloseMessage() error                    { return ws.WriteCloseMessage_(ws) }
func (ws *MockWebsocket) WritePingMessage() error                     { return ws.WritePingMessage_(ws) }
func (ws *MockWebsocket) Close() error                                { return ws.Close_(ws) }
