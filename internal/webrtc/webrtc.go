package webrtc

import (
	"github.com/decentraland/communications-server-go/internal/logging"

	pion "github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"

	"github.com/pion/datachannel"
)

type OfferOptions = pion.OfferOptions
type PeerConnection = pion.PeerConnection
type DataChannel = pion.DataChannel
type ReadWriteCloser = datachannel.ReadWriteCloser

var config = pion.Configuration{
	ICEServers: []pion.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
}

type IWebRtc interface {
	NewConnection(peerAlias uint64) (*PeerConnection, error)
	CreateReliableDataChannel(conn *PeerConnection) (*DataChannel, error)
	CreateUnreliableDataChannel(conn *PeerConnection) (*DataChannel, error)
	RegisterOpenHandler(*DataChannel, func())
	Detach(*DataChannel) (ReadWriteCloser, error)
	CreateOffer(conn *PeerConnection) (string, error)
	OnAnswer(conn *PeerConnection, sdp string) error
	OnOffer(conn *PeerConnection, sdp string) (string, error)
	OnIceCandidate(conn *PeerConnection, sdp string) error
	IsClosed(conn *PeerConnection) bool
	IsNew(conn *PeerConnection) bool
	Close(conn *PeerConnection) error
}

type WebRtc struct{}

func MakeWebRtc() *WebRtc {
	return &WebRtc{}
}

func (w *WebRtc) NewConnection(peerAlias uint64) (*PeerConnection, error) {
	s := pion.SettingEngine{}

	s.LoggerFactory = &logging.PionLoggingFactory{PeerAlias: peerAlias}
	s.DetachDataChannels()

	api := pion.NewAPI(pion.WithSettingEngine(s))

	conn, err := api.NewPeerConnection(config)
	if err != nil {
		log.WithError(err).Error("cannot create a new webrtc connection")
		return nil, err
	}

	return conn, nil
}

func (w *WebRtc) IsClosed(conn *PeerConnection) bool {
	return conn.ConnectionState() == pion.PeerConnectionStateClosed
}

func (w *WebRtc) IsNew(conn *PeerConnection) bool {
	return conn.ICEConnectionState() == pion.ICEConnectionStateNew || conn.ICEConnectionState() == pion.ICEConnectionStateChecking
}

func (w *WebRtc) Close(conn *PeerConnection) error {
	return conn.Close()
}

func (w *WebRtc) CreateReliableDataChannel(conn *PeerConnection) (*DataChannel, error) {
	return conn.CreateDataChannel("reliable", nil)
}

func (w *WebRtc) CreateUnreliableDataChannel(conn *PeerConnection) (*DataChannel, error) {
	var maxRetransmits uint16 = 0
	var ordered bool = false
	options := &pion.DataChannelInit{
		MaxRetransmits: &maxRetransmits,
		Ordered:        &ordered,
	}

	return conn.CreateDataChannel("unreliable", options)
}

func (w *WebRtc) RegisterOpenHandler(dc *DataChannel, handler func()) {
	dc.OnOpen(handler)
}

func (w *WebRtc) Detach(dc *DataChannel) (ReadWriteCloser, error) {
	return dc.Detach()
}

func (w *WebRtc) CreateOffer(conn *PeerConnection) (string, error) {
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

func (w *WebRtc) OnAnswer(conn *PeerConnection, sdp string) error {
	answer := pion.SessionDescription{
		Type: pion.SDPTypeAnswer,
		SDP:  sdp,
	}

	if err := conn.SetRemoteDescription(answer); err != nil {
		return err
	}

	return nil
}

func (w *WebRtc) OnOffer(conn *PeerConnection, sdp string) (string, error) {
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

func (w *WebRtc) OnIceCandidate(conn *PeerConnection, sdp string) error {
	if err := conn.AddICECandidate(pion.ICECandidateInit{Candidate: sdp}); err != nil {
		return err
	}

	return nil
}
