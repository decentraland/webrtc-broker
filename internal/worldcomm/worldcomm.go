package worldcomm

import (
	"errors"
	"log"
	"time"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/webrtc"
	"github.com/decentraland/communications-server-go/internal/ws"
	"github.com/golang/protobuf/proto"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	reportPeriod   = 60 * time.Second
	maxMessageSize = 1536 // NOTE let's adjust this later
	commRadius     = 10
	minParcelX     = -3000
	minParcelZ     = -3000
	maxParcelX     = 3000
	maxParcelZ     = 3000
)

func max(a int32, b int32) int32 {
	if a > b {
		return a
	} else {
		return b
	}
}

func min(a int32, b int32) int32 {
	if a < b {
		return a
	} else {
		return b
	}
}

func Now() (time.Time, float64) {
	now := time.Now()
	return now, float64(now.UnixNano() / int64(time.Millisecond))
}

type parcel struct {
	x int32
	z int32
}

type commarea struct {
	vMin parcel
	vMax parcel
}

func (ca *commarea) contains(p parcel) bool {
	vMin := ca.vMin
	vMax := ca.vMax
	return p.x >= vMin.x && p.z >= vMin.z && p.x <= vMax.x && p.z <= vMax.z
}

func makeCommArea(center parcel) commarea {
	return commarea{
		vMin: parcel{max(center.x-commRadius, minParcelX), max(center.z-commRadius, minParcelZ)},
		vMax: parcel{min(center.x+commRadius, maxParcelX), min(center.z+commRadius, maxParcelZ)},
	}
}

type clientPosition struct {
	quaternion [7]float32
	parcel     parcel
	lastUpdate time.Time
}

type client struct {
	conn             ws.IWebsocket
	alias            uint32
	peerId           string
	position         *clientPosition
	flowStatus       FlowStatus
	send             chan *outMessage
	webRtcConnection *webrtc.Connection
}

func makeClient(conn ws.IWebsocket) *client {
	return &client{
		conn:       conn,
		position:   nil,
		flowStatus: FlowStatus_UNKNOWN_STATUS,
		send:       make(chan *outMessage, 256),
	}
}

type inMessage struct {
	msgType   MessageType
	c         *client
	tReceived time.Time
	tMsg      time.Time
	bytes     []byte
}

type outMessage struct {
	tryWebRtc bool
	tReceived *time.Time
	bytes     []byte
}

type worldCommunicationState struct {
	clients            map[*client]bool
	pingQueue          chan *inMessage
	positionQueue      chan *inMessage
	chatQueue          chan *inMessage
	profileQueue       chan *inMessage
	flowStatusQueue    chan *inMessage
	webRtcControlQueue chan *inMessage
	register           chan *client
	unregister         chan *client

	agent     agent.IAgent
	nextAlias uint32

	transient struct {
		positionMessage *PositionMessage
	}
}

func makeState(agent agent.IAgent) worldCommunicationState {
	return worldCommunicationState{
		clients:            make(map[*client]bool),
		pingQueue:          make(chan *inMessage),
		positionQueue:      make(chan *inMessage),
		chatQueue:          make(chan *inMessage),
		profileQueue:       make(chan *inMessage),
		flowStatusQueue:    make(chan *inMessage),
		webRtcControlQueue: make(chan *inMessage),
		register:           make(chan *client),
		unregister:         make(chan *client),
		agent:              agent,
	}
}

func connect(state *worldCommunicationState, conn ws.IWebsocket) {
	log.Println("socket connect")
	c := makeClient(conn)
	state.register <- c
	go read(state, c)
	go write(state, c)
}

func closeState(state *worldCommunicationState) {
	close(state.pingQueue)
	close(state.positionQueue)
	close(state.chatQueue)
	close(state.profileQueue)
	close(state.flowStatusQueue)
	close(state.webRtcControlQueue)
	close(state.register)
	close(state.unregister)
}

func process(state *worldCommunicationState) {
	for {
		processQueues(state)
	}
}

func processQueues(state *worldCommunicationState) {
	ticker := time.NewTicker(reportPeriod)
	defer func() {
		ticker.Stop()
	}()

	select {
	case c := <-state.register:
		register(state, c)
		state.agent.RecordTotalConnections(len(state.clients))
	case c := <-state.unregister:
		unregister(state, c)
		state.agent.RecordTotalConnections(len(state.clients))
	case <-ticker.C:
		state.agent.RecordTotalConnections(len(state.clients))
	case in := <-state.pingQueue:
		in.c.send <- &outMessage{bytes: in.bytes}
		n := len(state.pingQueue)
		for i := 0; i < n; i++ {
			in := <-state.pingQueue
			in.c.send <- &outMessage{bytes: in.bytes}
		}
	case in := <-state.positionQueue:
		processPositionMessage(state, in)
		n := len(state.positionQueue)
		for i := 0; i < n; i++ {
			in := <-state.positionQueue
			processPositionMessage(state, in)
		}
	case in := <-state.chatQueue:
		message := &ChatMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode chat message", err)
			break
		}

		size := len(in.bytes)
		state.agent.RecordRecvChatSize(size)
		state.agent.RecordRecvSize(size)

		message.Alias = in.c.alias
		out, err := makeOutMessage(&in.tReceived, message)
		if err != nil {
			break
		}

		broadcast(state, in.c, out)
	case in := <-state.profileQueue:
		message := &ProfileMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode profile message", err)
			break
		}

		size := len(in.bytes)
		state.agent.RecordRecvProfileSize(size)
		state.agent.RecordRecvSize(size)
		c := in.c
		if c.peerId == "" {
			c.peerId = message.GetPeerId()
		}

		message.Alias = c.alias

		out, err := makeOutMessage(&in.tReceived, message)
		if err != nil {
			return
		}

		broadcast(state, c, out)
	case in := <-state.flowStatusQueue:
		processFlowStatusMessage(state, in)
	case in := <-state.webRtcControlQueue:
		processWebRtcControlMessage(state, in)
		n := len(state.webRtcControlQueue)
		for i := 0; i < n; i++ {
			in := <-state.webRtcControlQueue
			processWebRtcControlMessage(state, in)
		}
	}

}

func register(state *worldCommunicationState, c *client) {
	c.alias = state.nextAlias
	state.nextAlias += 1
	state.clients[c] = true
}

func unregister(state *worldCommunicationState, c *client) {
	delete(state.clients, c)
	close(c.send)

	if c.webRtcConnection != nil {
		c.webRtcConnection.Close()
		c.webRtcConnection = nil
	}

	_, ms := Now()
	message := &ClientDisconnectedFromServerMessage{
		Type:  MessageType_CLIENT_DISCONNECTED_FROM_SERVER,
		Time:  ms,
		Alias: c.alias,
	}

	out, err := makeOutMessage(nil, message)
	if err != nil {
		return
	}

	broadcast(state, c, out)
}

func processPositionMessage(state *worldCommunicationState, in *inMessage) {
	c := in.c
	if state.transient.positionMessage == nil {
		state.transient.positionMessage = &PositionMessage{}
	}
	message := state.transient.positionMessage
	if err := proto.Unmarshal(in.bytes, message); err != nil {
		log.Println("Failed to decode position message", err)
		return
	}

	if c.position == nil || c.position.lastUpdate.Before(in.tMsg) {
		if c.position == nil {
			c.position = &clientPosition{}
		}
		c.position.quaternion = [7]float32{
			message.GetPositionX(),
			message.GetPositionY(),
			message.GetPositionZ(),
			message.GetRotationX(),
			message.GetRotationY(),
			message.GetRotationZ(),
			message.GetRotationW(),
		}
		c.position.parcel = parcel{
			x: int32(message.GetPositionX()),
			z: int32(message.GetPositionZ()),
		}
		c.position.lastUpdate = in.tMsg
	}

	size := len(in.bytes)
	state.agent.RecordRecvPositionSize(size)
	state.agent.RecordRecvSize(size)

	message.Alias = c.alias

	out, err := makeOutMessage(&in.tReceived, message)
	if err != nil {
		return
	}

	out.tryWebRtc = true
	broadcast(state, c, out)
}

func processFlowStatusMessage(state *worldCommunicationState, in *inMessage) {
	message := &FlowStatusMessage{}
	if err := proto.Unmarshal(in.bytes, message); err != nil {
		log.Println("Failed to decode flow status message", err)
		return
	}

	size := len(in.bytes)
	state.agent.RecordRecvFlowStatusSize(size)
	state.agent.RecordRecvSize(size)

	c := in.c

	flowStatus := message.GetFlowStatus()
	if flowStatus != FlowStatus_UNKNOWN_STATUS {
		c.flowStatus = flowStatus
		switch flowStatus {
		case FlowStatus_OPEN:
			if c.webRtcConnection != nil {
				c.webRtcConnection.Close()
				c.webRtcConnection = nil
			}
		case FlowStatus_OPEN_WEBRTC_PREFERRED:
			if c.webRtcConnection != nil {
				c.webRtcConnection.Close()
				c.webRtcConnection = nil
			}

			conn, err := webrtc.NewConnection()
			if err == nil {
				c.webRtcConnection = conn

				_, ms := Now()
				msg := &WebRtcSupportedMessage{Type: MessageType_WEBRTC_SUPPORTED, Time: ms}
				bytes, err := proto.Marshal(msg)
				if err != nil {
					log.Println("encode webrtc supported message failed", err)
					return
				}
				c.send <- &outMessage{bytes: bytes}

				_, ms = Now()
				offer, err := c.webRtcConnection.CreateOffer()
				if err != nil {
					log.Println("cannot create offer", err)
					return
				}
				offerMsg := &WebRtcOfferMessage{Type: MessageType_WEBRTC_OFFER, Time: ms, Sdp: offer}
				bytes, err = proto.Marshal(offerMsg)
				if err != nil {
					log.Println("encode webrtc offer message failed", err)
					return
				}
				c.send <- &outMessage{bytes: bytes}
			} else {
				log.Println("error creating new peer connection", err)
			}
		case FlowStatus_CLOSE:
			if c.webRtcConnection != nil {
				c.webRtcConnection.Close()
				c.webRtcConnection = nil
			}
		}
	}
}

func processWebRtcControlMessage(state *worldCommunicationState, in *inMessage) {
	c := in.c

	size := len(in.bytes)
	state.agent.RecordRecvWebRtcAnswerSize(size)
	state.agent.RecordRecvSize(size)

	switch in.msgType {
	case MessageType_WEBRTC_OFFER:
		message := &WebRtcOfferMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode webrtc offer message", err)
			return
		}

		answer, err := c.webRtcConnection.OnOffer(message.Sdp)
		if err != nil {
			log.Println("error setting webrtc offer", err)
			return
		}

		_, ms := Now()
		msg := &WebRtcAnswerMessage{Type: MessageType_WEBRTC_ANSWER, Time: ms, Sdp: answer}
		bytes, err := proto.Marshal(msg)
		if err != nil {
			log.Println("encode webrtc answer message failed", err)
			return
		}
		c.send <- &outMessage{bytes: bytes}
	case MessageType_WEBRTC_ANSWER:
		message := &WebRtcAnswerMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode webrtc answer message", err)
			return
		}

		if err := c.webRtcConnection.OnAnswer(message.Sdp); err != nil {
			log.Println("error setting webrtc answer", err)
			return
		}
	case MessageType_WEBRTC_ICE_CANDIDATE:
		message := &WebRtcIceCandidateMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode webrtc ice candidate message", err)
			return
		}

		if err := c.webRtcConnection.OnIceCandidate(message.Sdp); err != nil {
			log.Println("error setting webrtc answer", err)
			return
		}
	default:
		log.Fatal(errors.New("invalid message type in processWebRtcControlMessage"))
	}
}

func makeOutMessage(tReceived *time.Time, msg proto.Message) (*outMessage, error) {
	bytes, err := proto.Marshal(msg)
	if err != nil {
		log.Println("encode message failed", err)
		return nil, err
	}

	out := &outMessage{
		tReceived: tReceived,
		bytes:     bytes,
	}

	return out, nil
}

func broadcast(state *worldCommunicationState, from *client, out *outMessage) {
	if from.position == nil {
		return
	}

	t := time.Now()

	commArea := makeCommArea(from.position.parcel)

	for c := range state.clients {
		if c == from {
			continue
		}

		if c.position == nil {
			continue
		}

		if c.flowStatus != FlowStatus_OPEN && c.flowStatus != FlowStatus_OPEN_WEBRTC_PREFERRED {
			continue
		}

		if !commArea.contains(c.position.parcel) {
			continue
		}

		c.send <- out
	}

	state.agent.RecordBroadcastDuration(time.Since(t))
}

func read(state *worldCommunicationState, c *client) {
	defer func() {
		state.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(s string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	genericMessage := &GenericMessage{}
	for {
		bytes, err := c.conn.ReadMessage()
		if err != nil {
			if ws.IsUnexpectedCloseError(err) {
				log.Printf("unexcepted close error: %v", err)
			}
			log.Printf("read error: %v", err)
			break
		}

		if err := proto.Unmarshal(bytes, genericMessage); err != nil {
			log.Println("Failed to load:", err)
			continue
		}

		ts := time.Unix(0, int64(genericMessage.GetTime())*int64(time.Millisecond))
		// if ts.After(time.Now()) {
		// 	// TODO
		// 	continue
		// }
		msgType := genericMessage.GetType()

		in := &inMessage{
			msgType:   msgType,
			tReceived: time.Now(),
			tMsg:      ts,
			c:         c,
			bytes:     bytes,
		}

		switch msgType {
		case MessageType_FLOW_STATUS:
			state.flowStatusQueue <- in
		case MessageType_CHAT:
			state.chatQueue <- in
		case MessageType_PING:
			state.pingQueue <- in
		case MessageType_POSITION:
			state.positionQueue <- in
		case MessageType_PROFILE:
			state.profileQueue <- in
		case MessageType_WEBRTC_OFFER:
			state.webRtcControlQueue <- in
		case MessageType_WEBRTC_ANSWER:
			state.webRtcControlQueue <- in
		case MessageType_WEBRTC_ICE_CANDIDATE:
			state.webRtcControlQueue <- in
		default:
			log.Println("unhandled message", msgType)
		}
	}
}

func write(state *worldCommunicationState, c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	writeMessage := func(out *outMessage) error {
		if out.tryWebRtc && c.webRtcConnection != nil && c.webRtcConnection.Ready {
			c.webRtcConnection.Write(out.bytes)
		} else {
			err := c.conn.WriteMessage(out.bytes)
			if err != nil {
				return err
			}
		}

		state.agent.RecordSentSize(len(out.bytes))
		if out.tReceived != nil {
			d := time.Since(*out.tReceived)
			state.agent.RecordEndToEndDuration(d)
		}

		return nil
	}

	for {
		select {
		case out, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteCloseMessage()
				return
			}
			if err := writeMessage(out); err != nil {
				log.Println("error writing message", err)
				return
			}

			n := len(c.send)
			for i := 0; i < n; i++ {
				out = <-c.send
				if err := writeMessage(out); err != nil {
					log.Println("error writing message", err)
					return
				}
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WritePingMessage(); err != nil {
				log.Println("error writing ping message", err)
				return
			}
		}
	}
}

type IWorldComm interface {
	Connect(conn ws.IWebsocket)
	Process()
	Close()
}

type worldComm struct {
	state worldCommunicationState
}

func (wc *worldComm) Connect(conn ws.IWebsocket) {
	connect(&wc.state, conn)
}

func (wc *worldComm) Process() {
	process(&wc.state)
}

func (wc *worldComm) Close() {
	closeState(&wc.state)
}

func Make(agent agent.IAgent) IWorldComm {
	state := makeState(agent)
	return &worldComm{state: state}
}
