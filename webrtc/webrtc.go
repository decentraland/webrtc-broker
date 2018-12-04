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

func NewConnection() (*Connection, string, error) {
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
		return nil, "", err
	}

	connection := &Connection{conn: conn, createdAt: time.Now()}
	var maxRetransmits uint16 = 0
	options := &_webrtc.RTCDataChannelInit{MaxRetransmits: &maxRetransmits}
	channel, err := conn.CreateDataChannel("data", options)
	if err != nil {
		log.Println("cannot create new data channel", err)
		return nil, "", err
	}
	connection.dataChannel = channel

	conn.OnICEConnectionStateChange(func(connectionState ice.ConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())

		connection.Ready = connectionState == ice.ConnectionStateConnected
	})

	channel.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", channel.Label, channel.ID)
	})

	offer, err := conn.CreateOffer(nil)

	if err != nil {
		log.Println("error creating webrtc offer", err)
		return nil, "", err
	}

	return connection, offer.Sdp, nil
}

func (conn *Connection) OnAnswer(sdp string) error {
	answer := _webrtc.RTCSessionDescription{
		Type: _webrtc.RTCSdpTypeAnswer,
		Sdp:  sdp,
	}

	err := conn.conn.SetRemoteDescription(answer)

	if err != nil {
		log.Println("error setting remove description", err)
	}
	return err
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
