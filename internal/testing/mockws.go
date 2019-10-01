// Package testing contains internal testing utilities
package testing

import (
	"time"

	"github.com/stretchr/testify/mock"
)

// MockWebsocket mocks a websocket
type MockWebsocket struct {
	mock.Mock
}

// SetReadDeadline sets read deadline
func (m *MockWebsocket) SetReadDeadline(t time.Time) error {
	args := m.Called(t)
	return args.Error(0)
}

// SetReadLimit sets read limit
func (m *MockWebsocket) SetReadLimit(l int64) {
	m.Called(l)
}

// SetPongHandler sets pong handler
func (m *MockWebsocket) SetPongHandler(h func(appData string) error) {
	m.Called(h)
}

// ReadMessage read socket message
func (m *MockWebsocket) ReadMessage() ([]byte, error) {
	args := m.Called()
	return args.Get(0).([]byte), args.Error(1)
}

// WriteMessage writes a message to the ws
func (m *MockWebsocket) WriteMessage(data []byte) error {
	args := m.Called(data)
	return args.Error(0)
}

// WritePingMessage writes a ping message to the ws
func (m *MockWebsocket) WritePingMessage() error {
	args := m.Called()
	return args.Error(0)
}

// WriteCloseMessage writes a close message to the ws
func (m *MockWebsocket) WriteCloseMessage() error {
	args := m.Called()
	return args.Error(0)
}

// Close closes the ws
func (m *MockWebsocket) Close() error {
	args := m.Called()
	return args.Error(0)
}
