package server

import (
	"encoding/json"
	"errors"
	"io"
	"time"

	"testing"

	pion "github.com/pion/webrtc/v2"
	"github.com/stretchr/testify/mock"

	"github.com/decentraland/webrtc-broker/internal/logging"
	_testing "github.com/decentraland/webrtc-broker/internal/testing"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

type MockWebsocket = _testing.MockWebsocket

type mockWebRtc struct {
	mock.Mock
}

func (m *mockWebRtc) newConnection(peerAlias uint64) (*PeerConnection, error) {
	args := m.Called(peerAlias)
	return args.Get(0).(*PeerConnection), args.Error(1)
}

func (m *mockWebRtc) createOffer(conn *PeerConnection) (pion.SessionDescription, error) {
	args := m.Called(conn)
	return args.Get(0).(pion.SessionDescription), args.Error(1)
}

func (m *mockWebRtc) onAnswer(conn *PeerConnection, answer pion.SessionDescription) error {
	args := m.Called(conn, answer)
	return args.Error(0)
}

func (m *mockWebRtc) onOffer(conn *PeerConnection, offer pion.SessionDescription) (pion.SessionDescription, error) {
	args := m.Called(conn, offer)
	return args.Get(0).(pion.SessionDescription), args.Error(1)
}

func (m *mockWebRtc) onIceCandidate(conn *PeerConnection, candidate pion.ICECandidateInit) error {
	args := m.Called(conn, candidate)
	return args.Error(0)
}

func (m *mockWebRtc) isClosed(conn *PeerConnection) bool {
	args := m.Called(conn)
	return args.Bool(0)
}

func (m *mockWebRtc) isNew(conn *PeerConnection) bool {
	args := m.Called(conn)
	return args.Bool(0)
}

func (m *mockWebRtc) close(conn io.Closer) error {
	args := m.Called(conn)
	return args.Error(0)
}

func (m *mockWebRtc) getStats(conn *PeerConnection) pion.StatsReport {
	args := m.Called(conn)
	return args.Get(0).(pion.StatsReport)
}

func addPeer(s *Server, alias uint64) *Peer {
	p := &Peer{
		Alias:        alias,
		Conn:         &pion.PeerConnection{},
		unregisterCh: s.unregisterCh,
		webRtc:       s.webRtc,
	}
	s.peers = append(s.peers, p)

	return p
}

func TestCoordinatorSend(t *testing.T) {
	c := coordinator{send: make(chan []byte, 256), log: logging.New()}
	defer c.Close()

	msg1 := &protocol.PingMessage{}
	encoded1, err := proto.Marshal(msg1)
	require.NoError(t, err)
	require.NoError(t, c.Send(msg1))
	require.Len(t, c.send, 1)

	msg2 := &protocol.PingMessage{}
	encoded2, err := proto.Marshal(msg2)
	require.NoError(t, err)
	require.NoError(t, c.Send(msg2))
	require.Len(t, c.send, 2)

	require.Equal(t, <-c.send, encoded2)
	require.Equal(t, <-c.send, encoded1)
}

func TestCoordinatorReadPump(t *testing.T) {
	t.Run("welcome server message", func(t *testing.T) {
		s, err := NewServer(&Config{})
		require.NoError(t, err)

		conn := &MockWebsocket{}
		s.coordinator.conn = conn
		msg := &protocol.WelcomeMessage{
			Type:             protocol.MessageType_WELCOME,
			Alias:            3,
			AvailableServers: []uint64{1, 2},
		}
		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		conn.
			On("Close").Return(nil).
			On("ReadMessage").Return(encodedMsg, nil).Once().
			On("ReadMessage").Return([]byte{}, errors.New("stop")).Once().
			On("SetReadLimit", mock.Anything).Return(nil).Once().
			On("SetReadDeadline", mock.Anything).Return(nil).Once().
			On("SetPongHandler", mock.Anything).Once()

		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go s.coordinator.readPump(s, welcomeChannel)

		welcomeMessage := <-welcomeChannel

		require.Equal(t, uint64(3), welcomeMessage.Alias)
	})

	t.Run("webrtc message", func(t *testing.T) {
		s, err := NewServer(&Config{})
		require.NoError(t, err)

		conn := &MockWebsocket{}
		s.coordinator.conn = conn
		msg := &protocol.WebRtcMessage{
			Type: protocol.MessageType_WEBRTC_ANSWER,
		}
		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		conn.
			On("Close").Return(nil).
			On("ReadMessage").Return(encodedMsg, nil).Once().
			On("ReadMessage").Return([]byte{}, errors.New("stop")).Once().
			On("SetReadLimit", mock.Anything).Return(nil).Once().
			On("SetReadDeadline", mock.Anything).Return(nil).Once().
			On("SetPongHandler", mock.Anything).Once()
		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go s.coordinator.readPump(s, welcomeChannel)

		<-s.coordinator.send

		require.Len(t, s.webRtcControlCh, 1)
	})

	t.Run("connect message", func(t *testing.T) {
		s, err := NewServer(&Config{})
		require.NoError(t, err)

		conn := &MockWebsocket{}
		s.coordinator.conn = conn
		msg := &protocol.ConnectMessage{
			Type:      protocol.MessageType_CONNECT,
			FromAlias: 2,
		}
		encodedMsg, err := proto.Marshal(msg)
		require.NoError(t, err)

		conn.
			On("Close").Return(nil).
			On("ReadMessage").Return(encodedMsg, nil).Once().
			On("ReadMessage").Return([]byte{}, errors.New("stop")).Once().
			On("SetReadLimit", mock.Anything).Return(nil).Once().
			On("SetReadDeadline", mock.Anything).Return(nil).Once().
			On("SetPongHandler", mock.Anything).Once()
		welcomeChannel := make(chan *protocol.WelcomeMessage)
		go s.coordinator.readPump(s, welcomeChannel)

		<-s.coordinator.send

		require.Len(t, s.connectCh, 1)
		require.Equal(t, uint64(2), <-s.connectCh)
	})
}

func TestCoordinatorWritePump(t *testing.T) {
	msg, err := proto.Marshal(&protocol.PingMessage{})
	require.NoError(t, err)

	conn := &MockWebsocket{}
	conn.
		On("Close").Return(nil).
		On("WriteMessage", msg).Return(nil).Once().
		On("WriteMessage", msg).Return(errors.New("stop")).Once()

	c := coordinator{
		send: make(chan []byte, 256),
		log:  logging.New(),
		conn: conn,
	}

	c.send <- msg
	c.send <- msg

	c.writePump()
	conn.AssertExpectations(t)
}

func newConnection(t *testing.T) *pion.PeerConnection {
	s := pion.SettingEngine{}
	api := pion.NewAPI(pion.WithSettingEngine(s))
	conn, err := api.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)

	return conn
}

func TestProcessConnect(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		conn1 := newConnection(t)
		conn2 := newConnection(t)

		offer := pion.SessionDescription{}
		webRtc := &mockWebRtc{}
		webRtc.
			On("newConnection", uint64(1)).Return(conn1, nil).Once().
			On("isNew", conn1).Return(true).Maybe().
			On("close", conn1).Return(nil).Maybe().
			On("createOffer", conn1).Return(offer, nil).Once().
			On("newConnection", uint64(2)).Return(conn2, nil).Once().
			On("isNew", conn2).Return(true).Maybe().
			On("close", conn2).Return(nil).Maybe().
			On("createOffer", conn2).Return(offer, nil).Once()

		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		s.connectCh <- 1
		s.connectCh <- 2

		close(s.connectCh)

		s.ProcessControlMessages()

		// NOTE: eventually connections will be closed because establish timeout
		<-s.unregisterCh
		<-s.unregisterCh

		require.Len(t, s.coordinator.send, 2)
		webRtc.AssertExpectations(t)
	})

	t.Run("create offer error", func(t *testing.T) {
		conn := newConnection(t)

		webRtc := &mockWebRtc{}
		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(true).Maybe().
			On("close", conn).Return(nil).Maybe().
			On("createOffer", conn).Return(pion.SessionDescription{}, errors.New("cannot create offer")).Once()

		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		s.connectCh <- 1
		close(s.connectCh)

		s.ProcessControlMessages()

		// NOTE: eventually connection will be closed because establish timeout
		<-s.unregisterCh

		require.Len(t, s.coordinator.send, 0)
		webRtc.AssertExpectations(t)
	})
}

func TestUnregister(t *testing.T) {
	webRtc := &mockWebRtc{}
	s, err := NewServer(&Config{WebRtc: webRtc})
	require.NoError(t, err)

	p := addPeer(s, 1)
	p2 := addPeer(s, 2)

	webRtc.
		On("close", p.Conn).Return(errors.New("connection closed")).Once().
		On("close", p2.Conn).Return(errors.New("connection closed")).Once()

	s.unregisterCh <- p
	s.unregisterCh <- p2
	close(s.unregisterCh)

	s.ProcessControlMessages()

	require.Len(t, s.peers, 0)
}

func TestProcessWebRtcMessage(t *testing.T) {
	t.Run("webrtc offer (on a new peer)", func(t *testing.T) {
		conn := newConnection(t)

		offer := pion.SessionDescription{
			Type: pion.SDPTypeOffer,
			SDP:  "sdp",
		}

		webRtc := &mockWebRtc{}
		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).Once().
			On("isNew", conn).Return(true).Maybe().
			On("close", conn).Return(nil).
			On("onOffer", conn, offer).Return(pion.SessionDescription{}, nil).Once()

		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		data, err := json.Marshal(offer)
		require.NoError(t, err)

		s.webRtcControlCh <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Data:      data,
			FromAlias: 1,
		}
		close(s.webRtcControlCh)

		s.ProcessControlMessages()

		// NOTE: eventually connection will be closed because establish timeout
		<-s.unregisterCh

		require.Len(t, s.coordinator.send, 1)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc offer", func(t *testing.T) {
		offer := pion.SessionDescription{
			Type: pion.SDPTypeOffer,
			SDP:  "sdp",
		}

		webRtc := &mockWebRtc{}
		webRtc.
			On("close", mock.Anything).Return(nil).Twice().
			On("onOffer", mock.Anything, offer).Return(pion.SessionDescription{}, nil).Twice()

		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		p := addPeer(s, 1)
		p2 := addPeer(s, 2)

		data, err := json.Marshal(offer)
		require.NoError(t, err)

		s.webRtcControlCh <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Data:      data,
			FromAlias: p.Alias,
		}

		s.webRtcControlCh <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Data:      data,
			FromAlias: p2.Alias,
		}
		close(s.webRtcControlCh)

		s.ProcessControlMessages()

		require.Len(t, s.coordinator.send, 2)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc offer (offer error)", func(t *testing.T) {
		offer := pion.SessionDescription{
			Type: pion.SDPTypeOffer,
			SDP:  "sdp",
		}

		webRtc := &mockWebRtc{}
		webRtc.
			On("close", mock.Anything).Return(nil).
			On("onOffer", mock.Anything, offer).
			Return(pion.SessionDescription{}, errors.New("offer error")).
			Once()
		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		p := addPeer(s, 1)

		data, err := json.Marshal(offer)
		require.NoError(t, err)

		s.webRtcControlCh <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_OFFER,
			Data:      data,
			FromAlias: p.Alias,
		}
		close(s.webRtcControlCh)

		s.ProcessControlMessages()

		require.Len(t, s.coordinator.send, 0)
		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc answer", func(t *testing.T) {
		answer := pion.SessionDescription{
			Type: pion.SDPTypeAnswer,
			SDP:  "sdp",
		}

		webRtc := &mockWebRtc{}
		webRtc.
			On("onAnswer", mock.Anything, answer).Return(nil).Once().
			On("close", mock.Anything).Return(nil)
		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		p := addPeer(s, 1)

		data, err := json.Marshal(answer)
		require.NoError(t, err)

		s.webRtcControlCh <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ANSWER,
			Data:      data,
			FromAlias: p.Alias,
		}
		close(s.webRtcControlCh)

		s.ProcessControlMessages()

		webRtc.AssertExpectations(t)
	})

	t.Run("webrtc ice candidate", func(t *testing.T) {
		candidate := pion.ICECandidateInit{Candidate: "sdp-candidate"}
		webRtc := &mockWebRtc{}
		webRtc.
			On("onIceCandidate", mock.Anything, candidate).Return(nil).Once().
			On("close", mock.Anything).Return(nil)

		s, err := NewServer(&Config{WebRtc: webRtc})
		require.NoError(t, err)

		p := addPeer(s, 1)

		data, err := json.Marshal(candidate)
		require.NoError(t, err)
		s.webRtcControlCh <- &protocol.WebRtcMessage{
			Type:      protocol.MessageType_WEBRTC_ICE_CANDIDATE,
			Data:      data,
			FromAlias: p.Alias,
		}
		close(s.webRtcControlCh)

		s.ProcessControlMessages()

		webRtc.AssertExpectations(t)
	})
}

func TestInitPeer(t *testing.T) {
	t.Run("if no connection is establish eventually the peer is unregistered", func(t *testing.T) {
		webRtc := &mockWebRtc{}
		conn := newConnection(t)

		webRtc.
			On("newConnection", uint64(1)).Return(conn, nil).
			On("isNew", conn).Return(true).
			On("close", conn).Return(nil).
			On("close", conn).Return(errors.New("already closed"))

		s, err := NewServer(&Config{
			WebRtc:                  webRtc,
			EstablishSessionTimeout: 10 * time.Millisecond,
		})
		require.NoError(t, err)

		_, err = s.initPeer(1)
		require.NoError(t, err)

		p := <-s.unregisterCh
		require.Equal(t, uint64(1), p.Alias)
	})
}
