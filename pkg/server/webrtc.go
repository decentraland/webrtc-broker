package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"time"

	"github.com/rs/zerolog"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/pion/datachannel"
	pion "github.com/pion/webrtc/v2"
)

// PeerConnection represents the webrtc connection
type PeerConnection = pion.PeerConnection

// DataChannel is the top level pion's data channel
type DataChannel = pion.DataChannel

// DataChannelState is the pion's data channel state
type DataChannelState = pion.DataChannelState

// ReadWriteCloser is the detached datachannel pion's interface for read/write
type ReadWriteCloser = datachannel.ReadWriteCloser

// ICEServer represents a ICEServer config
type ICEServer = pion.ICEServer

// ICEConnectionState is the pion's ICEConnectionState
type ICEConnectionState = pion.ICEConnectionState

// ICECandidateType is the pion's ICECandidateType
type ICECandidateType = pion.ICECandidateType

// ICECandidate is the pion's ICECandidate
type ICECandidate = pion.ICECandidate

// IWebRtc is this module interface
type IWebRtc interface {
	newConnection(peerAlias uint64) (*PeerConnection, error)
	createOffer(conn *PeerConnection) (pion.SessionDescription, error)
	onAnswer(conn *PeerConnection, answer pion.SessionDescription) error
	onOffer(conn *PeerConnection, offer pion.SessionDescription) (pion.SessionDescription, error)
	onIceCandidate(conn *PeerConnection, candidate pion.ICECandidateInit) error
	isClosed(conn *PeerConnection) bool
	isNew(conn *PeerConnection) bool
	close(conn io.Closer) error
	getStats(conn *PeerConnection) pion.StatsReport
}

// WebRtc is our inmplemenation of IWebRtc
type webRTC struct {
	ICEServers  []ICEServer
	certificate *pion.Certificate
	LogLevel    zerolog.Level
}

func (w *webRTC) getCertificates() ([]pion.Certificate, error) {
	if w.certificate == nil ||
		(!w.certificate.Expires().IsZero() && time.Now().After(w.certificate.Expires())) {
		// dtls cert
		sk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}

		w.certificate, err = pion.GenerateCertificate(sk)
		if err != nil {
			return nil, err
		}
	}

	return []pion.Certificate{*w.certificate}, nil
}

func (w *webRTC) newConnection(peerAlias uint64) (*PeerConnection, error) {
	s := pion.SettingEngine{}
	s.SetCandidateSelectionTimeout(20 * time.Second)
	s.SetHostAcceptanceMinWait(0)
	s.SetSrflxAcceptanceMinWait(0)
	s.SetPrflxAcceptanceMinWait(0)
	s.SetRelayAcceptanceMinWait(5 * time.Second)
	s.SetTrickle(true)

	s.LoggerFactory = &logging.PionLoggingFactory{DefaultLogLevel: w.LogLevel, PeerAlias: peerAlias}
	s.DetachDataChannels()

	api := pion.NewAPI(pion.WithSettingEngine(s))

	certs, err := w.getCertificates()
	if err != nil {
		return nil, err
	}

	conn, err := api.NewPeerConnection(pion.Configuration{
		ICEServers:   w.ICEServers,
		Certificates: certs,
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
	return conn.ICEConnectionState() == pion.ICEConnectionStateNew ||
		conn.ICEConnectionState() == pion.ICEConnectionStateChecking
}

func (w *webRTC) close(conn io.Closer) error {
	return conn.Close()
}

func (w *webRTC) createOffer(conn *PeerConnection) (pion.SessionDescription, error) {
	offer, err := conn.CreateOffer(nil)
	if err != nil {
		return offer, err
	}

	err = conn.SetLocalDescription(offer)
	if err != nil {
		return offer, err
	}

	return offer, nil
}

func (w *webRTC) onAnswer(conn *PeerConnection, answer pion.SessionDescription) error {
	if err := conn.SetRemoteDescription(answer); err != nil {
		return err
	}

	return nil
}

func (w *webRTC) onOffer(conn *PeerConnection, offer pion.SessionDescription) (pion.SessionDescription, error) {
	if err := conn.SetRemoteDescription(offer); err != nil {
		return pion.SessionDescription{}, err
	}

	answer, err := conn.CreateAnswer(nil)
	if err != nil {
		return answer, err
	}

	err = conn.SetLocalDescription(answer)
	if err != nil {
		return answer, err
	}

	return answer, nil
}

func (w *webRTC) onIceCandidate(conn *PeerConnection, candidate pion.ICECandidateInit) error {
	if err := conn.AddICECandidate(candidate); err != nil {
		return err
	}

	return nil
}

func (w *webRTC) getStats(conn *PeerConnection) pion.StatsReport {
	return conn.GetStats()
}
