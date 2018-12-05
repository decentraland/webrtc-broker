package webrtc

import (
	"fmt"
	"log"
	"time"

	_webrtc "github.com/pions/webrtc"
	"github.com/pions/webrtc/pkg/datachannel"
	"github.com/pions/webrtc/pkg/ice"
)

type Connection struct {
	conn        *_webrtc.RTCPeerConnection
	dataChannel *_webrtc.RTCDataChannel
	createdAt   time.Time
	Ready       bool
}

func NewConnection() (*Connection, error) {
	config := _webrtc.RTCConfiguration{
		IceServers: []_webrtc.RTCIceServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	conn, err := _webrtc.New(config)
	if err != nil {
		log.Println("cannot create a new webrtc connection", err)
		return nil, err
	}

	connection := &Connection{conn: conn, createdAt: time.Now()}
	var maxRetransmits uint16 = 0
	options := &_webrtc.RTCDataChannelInit{MaxRetransmits: &maxRetransmits}
	channel, err := conn.CreateDataChannel("data", options)
	if err != nil {
		log.Println("cannot create new data channel", err)
		return nil, err
	}
	connection.dataChannel = channel

	conn.OnICEConnectionStateChange(func(connectionState ice.ConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())

		connection.Ready = connectionState == ice.ConnectionStateConnected
	})

	channel.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", channel.Label, channel.ID)
	})

	return connection, nil
}

func (conn *Connection) CreateOffer() (string, error) {
	offer, err := conn.conn.CreateOffer(nil)

	if err != nil {
		log.Println("error creating webrtc offer", err)
		return "", err
	}

	return offer.Sdp, nil
}

func (conn *Connection) OnAnswer(sdp string) error {
	answer := _webrtc.RTCSessionDescription{
		Type: _webrtc.RTCSdpTypeAnswer,
		Sdp:  sdp,
	}

	if err := conn.conn.SetRemoteDescription(answer); err != nil {
		log.Println("error setting remove description", err)
	}

	return nil
}

func (conn *Connection) OnOffer(sdp string) (string, error) {
	answer := _webrtc.RTCSessionDescription{
		Type: _webrtc.RTCSdpTypeAnswer,
		Sdp:  sdp,
	}

	if err := conn.conn.SetRemoteDescription(answer); err != nil {
		log.Println("error setting remove description", err)
		return "", err
	}

	answer, err := conn.conn.CreateAnswer(nil)

	if err != nil {
		log.Println("error creating webrtc answer", err)
		return "", err
	}

	return answer.Sdp, nil
}

func (conn *Connection) OnIceCandidate(sdp string) error {
	if err := conn.conn.AddIceCandidate(sdp); err != nil {
		log.Println("error adding ice candidate", err)
		return err
	}

	return nil
}

func (conn *Connection) Write(bytes []byte) error {
	payload := datachannel.PayloadBinary{Data: bytes}
	if err := conn.dataChannel.Send(payload); err != nil {
		log.Println("error writing to data channel", err)
		return err
	}

	return nil
}

func (conn *Connection) Close() {
	conn.Ready = false
	if err := conn.conn.Close(); err != nil {
		log.Println("error closing webrtc connection", err)
	}
}
