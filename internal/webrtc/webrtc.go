package webrtc

import (
	"errors"
	"time"

	"github.com/pions/datachannel"
	_webrtc "github.com/pions/webrtc"
	"github.com/pions/webrtc/pkg/ice"
	log "github.com/sirupsen/logrus"
)

type IWebRtcConnection interface {
	OnReliableChannelOpen(func())
	OnUnreliableChannelOpen(func())
	CreateOffer() (string, error)
	OnAnswer(sdp string) error
	OnOffer(sdp string) (string, error)
	OnIceCandidate(sdp string) error
	WriteReliable(bytes []byte) error
	WriteUnreliable(bytes []byte) error
	ReadReliable(bytes []byte) (int, error)
	ReadUnreliable(bytes []byte) (int, error)
	Close()
}

type webRtcConnection struct {
	onUnreliableChannelOpen func()
	onReliableChannelOpen   func()
	conn                    *_webrtc.PeerConnection
	reliableDataChannel     *datachannel.DataChannel
	unreliableDataChannel   *datachannel.DataChannel
	createdAt               time.Time
}

var config = _webrtc.Configuration{
	ICEServers: []_webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
}

func openUnreliableDataChannel(conn *webRtcConnection) error {
	var maxRetransmits uint16 = 0
	options := &_webrtc.DataChannelInit{MaxRetransmits: &maxRetransmits}
	c, err := conn.conn.CreateDataChannel("unreliable", options)

	if err != nil {
		log.WithField("error", err).Debug("cannot create new unreliable data channel")
		return err
	}

	c.OnOpen(func() {
		log.WithFields(log.Fields{
			"channel_label": c.Label,
			"channel_id":    *c.ID,
		}).Info("Unreliable data channel open")
		d, err := c.Detach()
		if err != nil {
			log.WithField("error", err).Error("cannot detach data channel")
			return
		}
		conn.unreliableDataChannel = d
		conn.onUnreliableChannelOpen()
	})

	return nil
}

func openReliableDataChannel(conn *webRtcConnection) error {
	c, err := conn.conn.CreateDataChannel("reliable", nil)
	if err != nil {
		log.WithField("error", err).Debug("cannot create new reliable data channel")
		return err
	}

	c.OnOpen(func() {
		log.WithFields(log.Fields{
			"channel_label": c.Label,
			"channel_id":    *c.ID,
		}).Info("Reliable data channel open")
		d, err := c.Detach()
		if err != nil {
			log.WithField("error", err).Error("cannot detach data channel")
			return
		}
		conn.reliableDataChannel = d
		conn.onReliableChannelOpen()
	})

	return nil
}

func (conn *webRtcConnection) OnReliableChannelOpen(l func()) {
	conn.onReliableChannelOpen = l
}

func (conn *webRtcConnection) OnUnreliableChannelOpen(l func()) {
	conn.onUnreliableChannelOpen = l
}

func (conn *webRtcConnection) CreateOffer() (string, error) {
	offer, err := conn.conn.CreateOffer(nil)
	if err != nil {
		log.WithError(err).Error("error creating webrtc offer")
		return "", err
	}

	err = conn.conn.SetLocalDescription(offer)
	if err != nil {
		log.WithError(err).Error("error setting local description, on create offer")
		return "", err
	}

	return offer.SDP, nil
}

func (conn *webRtcConnection) OnAnswer(sdp string) error {
	answer := _webrtc.SessionDescription{
		Type: _webrtc.SDPTypeAnswer,
		SDP:  sdp,
	}

	if err := conn.conn.SetRemoteDescription(answer); err != nil {
		log.WithError(err).Error("error setting remote description (on answer)")
		return err
	}

	return nil
}

func (conn *webRtcConnection) OnOffer(sdp string) (string, error) {
	offer := _webrtc.SessionDescription{
		Type: _webrtc.SDPTypeOffer,
		SDP:  sdp,
	}

	if err := conn.conn.SetRemoteDescription(offer); err != nil {
		log.WithError(err).Error("error setting remote description (on offer)")
		return "", err
	}

	answer, err := conn.conn.CreateAnswer(nil)
	if err != nil {
		log.WithError(err).Error("error creating webrtc answer")
		return "", err
	}

	err = conn.conn.SetLocalDescription(answer)
	if err != nil {
		log.WithError(err).Error("error setting local description, on OnOffer")
		return "", err
	}

	return answer.SDP, nil
}

func (conn *webRtcConnection) OnIceCandidate(sdp string) error {
	if err := conn.conn.AddICECandidate(sdp); err != nil {
		log.WithError(err).Error("error adding ice candidate")
		return err
	}

	return nil
}

func (conn *webRtcConnection) write(channel *datachannel.DataChannel, bytes []byte) error {
	if channel == nil {
		log.Debug("data channel is not open yet (nil)")
		return errors.New("data channal is yet not open")
	}

	_, err := channel.Write(bytes)
	if err != nil {
		log.WithField("error", err).Error("error writing to data channel")
		return err
	}

	return nil
}

func (conn *webRtcConnection) WriteReliable(bytes []byte) error {
	return conn.write(conn.reliableDataChannel, bytes)
}

func (conn *webRtcConnection) WriteUnreliable(bytes []byte) error {
	return conn.write(conn.unreliableDataChannel, bytes)
}

func (conn *webRtcConnection) ReadReliable(bytes []byte) (int, error) {
	if conn.reliableDataChannel != nil {
		return conn.reliableDataChannel.Read(bytes)
	}
	return 0, nil
}

func (conn *webRtcConnection) ReadUnreliable(bytes []byte) (int, error) {
	if conn.unreliableDataChannel != nil {
		return conn.unreliableDataChannel.Read(bytes)
	}
	return 0, nil
}

func (conn *webRtcConnection) Close() {
	if err := conn.conn.Close(); err != nil {
		// NOTE: this is not really an error, since it will fail if the connection is already dropped
		log.WithError(err).Debug("error closing webrtc connection")
	}
}

type IWebRtc interface {
	NewConnection() (IWebRtcConnection, error)
}

type WebRtc struct{}

func (*WebRtc) NewConnection() (IWebRtcConnection, error) {
	conn, err := _webrtc.NewPeerConnection(config)
	if err != nil {
		log.WithError(err).Error("cannot create a new webrtc connection")
		return nil, err
	}

	connection := &webRtcConnection{conn: conn, createdAt: time.Now()}

	if err := openReliableDataChannel(connection); err != nil {
		return nil, err
	}

	if err := openUnreliableDataChannel(connection); err != nil {
		return nil, err
	}

	conn.OnICEConnectionStateChange(func(connectionState ice.ConnectionState) {
		log.WithField("iceConnectionState", connectionState.String()).Debug("ICE Connection State has changed:")
		if connectionState == ice.ConnectionStateDisconnected {
			log.Debug("Connection state is disconnected, close connection")
			conn.Close()
		}
	})

	return connection, nil
}
