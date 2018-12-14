package testing

import (
	"github.com/decentraland/communications-server-go/internal/webrtc"
)

type MockWebRtcConnection struct {
	onUnreliableChannelOpen func()
	onReliableChannelOpen   func()
	CreateOffer_            func(conn *MockWebRtcConnection) (string, error)
	OnAnswer_               func(conn *MockWebRtcConnection, sdp string) error
	OnOffer_                func(conn *MockWebRtcConnection, sdp string) (string, error)
	OnIceCandidate_         func(conn *MockWebRtcConnection, sdp string) error
	WriteReliable_          func(conn *MockWebRtcConnection, bytes []byte) error
	WriteUnreliable_        func(conn *MockWebRtcConnection, bytes []byte) error
	ReadReliable_           func(conn *MockWebRtcConnection, bytes []byte) (int, error)
	ReadUnreliable_         func(conn *MockWebRtcConnection, bytes []byte) (int, error)
	Close_                  func(conn *MockWebRtcConnection)
}

func MakeMockWebRtcConnection() *MockWebRtcConnection {
	return &MockWebRtcConnection{
		Close_: func(conn *MockWebRtcConnection) {},
	}
}

func (conn *MockWebRtcConnection) OnReliableChannelOpen(l func()) {
	conn.onUnreliableChannelOpen = l
}

func (conn *MockWebRtcConnection) OnUnreliableChannelOpen(l func()) {
	conn.onReliableChannelOpen = l
}

func (conn *MockWebRtcConnection) CreateOffer() (string, error)       { return conn.CreateOffer_(conn) }
func (conn *MockWebRtcConnection) OnAnswer(sdp string) error          { return conn.OnAnswer_(conn, sdp) }
func (conn *MockWebRtcConnection) OnOffer(sdp string) (string, error) { return conn.OnOffer_(conn, sdp) }

func (conn *MockWebRtcConnection) OnIceCandidate(sdp string) error {
	return conn.OnIceCandidate_(conn, sdp)
}

func (conn *MockWebRtcConnection) WriteReliable(bytes []byte) error {
	return conn.WriteReliable_(conn, bytes)
}

func (conn *MockWebRtcConnection) WriteUnreliable(bytes []byte) error {
	return conn.WriteUnreliable_(conn, bytes)
}

func (conn *MockWebRtcConnection) ReadReliable(bytes []byte) (int, error) {
	return conn.ReadReliable_(conn, bytes)
}

func (conn *MockWebRtcConnection) ReadUnreliable(bytes []byte) (int, error) {
	return conn.ReadUnreliable_(conn, bytes)
}

func (conn *MockWebRtcConnection) Close() { conn.Close_(conn) }

type MockWebRtc struct {
	NewConnection_ func(*MockWebRtc) (webrtc.IWebRtcConnection, error)
}

func (w *MockWebRtc) NewConnection() (webrtc.IWebRtcConnection, error) {
	return w.NewConnection_(w)
}
