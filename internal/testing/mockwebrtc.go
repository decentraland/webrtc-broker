package testing

import (
	"github.com/decentraland/communications-server-go/internal/webrtc"
	"github.com/stretchr/testify/mock"
)

type MockReadWriteCloser struct {
	mock.Mock
}

func (m *MockReadWriteCloser) ReadDataChannel(p []byte) (int, bool, error) {
	n, err := m.Read(p)
	return n, false, err
}

func (m *MockReadWriteCloser) WriteDataChannel(p []byte, isString bool) (int, error) {
	return m.Write(p)
}

func (m *MockReadWriteCloser) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *MockReadWriteCloser) Write(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *MockReadWriteCloser) Close() error {
	args := m.Called()
	return args.Error(0)
}

type MockWebRtc struct {
	mock.Mock
}

func (m *MockWebRtc) NewConnection(peerAlias uint64) (*webrtc.PeerConnection, error) {
	args := m.Called(peerAlias)
	return args.Get(0).(*webrtc.PeerConnection), args.Error(1)
}

func (m *MockWebRtc) CreateReliableDataChannel(conn *webrtc.PeerConnection) (*webrtc.DataChannel, error) {
	args := m.Called(conn)
	return args.Get(0).(*webrtc.DataChannel), args.Error(1)
}

func (m *MockWebRtc) Detach(dc *webrtc.DataChannel) (webrtc.ReadWriteCloser, error) {
	args := m.Called(dc)
	return args.Get(0).(webrtc.ReadWriteCloser), args.Error(1)
}

func (m *MockWebRtc) CreateUnreliableDataChannel(conn *webrtc.PeerConnection) (*webrtc.DataChannel, error) {
	args := m.Called(conn)
	return args.Get(0).(*webrtc.DataChannel), args.Error(1)
}

func (m *MockWebRtc) RegisterOpenHandler(dc *webrtc.DataChannel, handler func()) {
	m.Called(dc, handler)
}

func (m *MockWebRtc) InvokeOpenHandler(dc *webrtc.DataChannel) {
	m.Called(dc)
}

func (m *MockWebRtc) CreateOffer(conn *webrtc.PeerConnection) (string, error) {
	args := m.Called(conn)
	return args.String(0), args.Error(1)
}

func (m *MockWebRtc) OnAnswer(conn *webrtc.PeerConnection, sdp string) error {
	args := m.Called(conn, sdp)
	return args.Error(0)
}

func (m *MockWebRtc) OnOffer(conn *webrtc.PeerConnection, sdp string) (string, error) {
	args := m.Called(conn, sdp)
	return args.String(0), args.Error(1)
}

func (m *MockWebRtc) OnIceCandidate(conn *webrtc.PeerConnection, sdp string) error {
	args := m.Called(conn, sdp)
	return args.Error(0)
}

func (m *MockWebRtc) IsClosed(conn *webrtc.PeerConnection) bool {
	args := m.Called(conn)
	return args.Bool(0)
}

func (m *MockWebRtc) IsNew(conn *webrtc.PeerConnection) bool {
	args := m.Called(conn)
	return args.Bool(0)
}

func (m *MockWebRtc) Close(conn *webrtc.PeerConnection) error {
	args := m.Called(conn)
	return args.Error(0)
}
