package commserver

import (
	"github.com/decentraland/webrtc-broker/internal/logging"

	pion "github.com/pion/webrtc/v2"

	"github.com/pion/datachannel"
)

// PeerConnection represents the webrtc connection
type PeerConnection = pion.PeerConnection

// DataChannel is the top level pion's data channel
type DataChannel = pion.DataChannel

// ReadWriteCloser is the detached datachannel pion's interface for read/write
type ReadWriteCloser = datachannel.ReadWriteCloser

// ICEServer represents a ICEServer config
type ICEServer = pion.ICEServer

// IWebRtc is this module interface
type IWebRtc interface {
	newConnection(peerAlias uint64) (*PeerConnection, error)
	createReliableDataChannel(conn *PeerConnection) (*DataChannel, error)
	createUnreliableDataChannel(conn *PeerConnection) (*DataChannel, error)
	registerOpenHandler(*DataChannel, func())
	detach(*DataChannel) (ReadWriteCloser, error)
	createOffer(conn *PeerConnection) (string, error)
	onAnswer(conn *PeerConnection, sdp string) error
	onOffer(conn *PeerConnection, sdp string) (string, error)
	onIceCandidate(conn *PeerConnection, sdp string) error
	isClosed(conn *PeerConnection) bool
	isNew(conn *PeerConnection) bool
	close(conn *PeerConnection) error
}

// WebRtc is our inmplemenation of IWebRtc
type webRTC struct {
	ICEServers []ICEServer
}

func (w *webRTC) newConnection(peerAlias uint64) (*PeerConnection, error) {
	s := pion.SettingEngine{}

	s.LoggerFactory = &logging.PionLoggingFactory{PeerAlias: peerAlias}
	s.DetachDataChannels()

	api := pion.NewAPI(pion.WithSettingEngine(s))

	conn, err := api.NewPeerConnection(pion.Configuration{
		ICEServers: w.ICEServers,
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func (w *webRTC) isClosed(conn *PeerConnection) bool {
	return conn.ConnectionState() == pion.PeerConnectionStateClosed
}

func (w *webRTC) isNew(conn *PeerConnection) bool {
	return conn.ICEConnectionState() == pion.ICEConnectionStateNew || conn.ICEConnectionState() == pion.ICEConnectionStateChecking
}

func (w *webRTC) close(conn *PeerConnection) error {
	return conn.Close()
}

func (w *webRTC) createReliableDataChannel(conn *PeerConnection) (*DataChannel, error) {
	return conn.CreateDataChannel("reliable", nil)
}

func (w *webRTC) createUnreliableDataChannel(conn *PeerConnection) (*DataChannel, error) {
	var maxRetransmits uint16
	var ordered = false
	options := &pion.DataChannelInit{
		MaxRetransmits: &maxRetransmits,
		Ordered:        &ordered,
	}

	return conn.CreateDataChannel("unreliable", options)
}

func (w *webRTC) registerOpenHandler(dc *DataChannel, handler func()) {
	dc.OnOpen(handler)
}

func (w *webRTC) detach(dc *DataChannel) (ReadWriteCloser, error) {
	return dc.Detach()
}

func (w *webRTC) createOffer(conn *PeerConnection) (string, error) {
	offer, err := conn.CreateOffer(nil)
	if err != nil {
		return "", err
	}

	err = conn.SetLocalDescription(offer)
	if err != nil {
		return "", err
	}

	return offer.SDP, nil
}

func (w *webRTC) onAnswer(conn *PeerConnection, sdp string) error {
	answer := pion.SessionDescription{
		Type: pion.SDPTypeAnswer,
		SDP:  sdp,
	}

	if err := conn.SetRemoteDescription(answer); err != nil {
		return err
	}

	return nil
}

func (w *webRTC) onOffer(conn *PeerConnection, sdp string) (string, error) {
	offer := pion.SessionDescription{
		Type: pion.SDPTypeOffer,
		SDP:  sdp,
	}

	if err := conn.SetRemoteDescription(offer); err != nil {
		return "", err
	}

	answer, err := conn.CreateAnswer(nil)
	if err != nil {
		return "", err
	}

	err = conn.SetLocalDescription(answer)
	if err != nil {
		return "", err
	}

	return answer.SDP, nil
}

func (w *webRTC) onIceCandidate(conn *PeerConnection, sdp string) error {
	if err := conn.AddICECandidate(pion.ICECandidateInit{Candidate: sdp}); err != nil {
		return err
	}

	return nil
}
