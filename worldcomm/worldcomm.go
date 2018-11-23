package worldcomm

import (
	"log"
	"time"

	"github.com/decentraland/communications-server-go/agent"
	"github.com/decentraland/communications-server-go/ws"
	"github.com/golang/protobuf/proto"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	reportPeriod   = 60 * time.Second
	maxMessageSize = 512 // NOTE let's adjust this later
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

func now() (time.Time, float64) {
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
	conn       ws.IWebsocket
	alias      uint32
	peerId     string
	position   *clientPosition
	flowStatus FlowStatus
	send       chan *outMessage
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
	c         *client
	tReceived time.Time
	tMsg      time.Time
	bytes     []byte
}

type outMessage struct {
	tReceived time.Time
	isSystem  bool
	bytes     []byte
}

type worldCommunicationState struct {
	clients         map[*client]bool
	pingQueue       chan *inMessage
	positionQueue   chan *inMessage
	chatQueue       chan *inMessage
	profileQueue    chan *inMessage
	flowStatusQueue chan *inMessage
	register        chan *client
	unregister      chan *client

	agent     agent.IAgent
	nextAlias uint32

	transient struct {
		positionMessage *PositionMessage
	}
}

func makeState(agent agent.IAgent) worldCommunicationState {
	return worldCommunicationState{
		clients:         make(map[*client]bool),
		pingQueue:       make(chan *inMessage),
		positionQueue:   make(chan *inMessage),
		chatQueue:       make(chan *inMessage),
		profileQueue:    make(chan *inMessage),
		flowStatusQueue: make(chan *inMessage),
		register:        make(chan *client),
		unregister:      make(chan *client),
		agent:           agent,
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
		in.c.send <- &outMessage{tReceived: in.tReceived, bytes: in.bytes, isSystem: true}
		n := len(state.pingQueue)
		for i := 0; i < n; i++ {
			in := <-state.pingQueue
			in.c.send <- &outMessage{tReceived: in.tReceived, bytes: in.bytes, isSystem: true}
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
			log.Println("Failed to decode chat message")
			return
		}

		size := len(in.bytes)
		state.agent.RecordRecvChatSize(size)
		state.agent.RecordRecvSize(size)

		message.Alias = in.c.alias
		broadcast(state, in.c, in.tReceived, message, false)
	case in := <-state.profileQueue:
		message := &ProfileMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode profile message")
			return
		}

		size := len(in.bytes)
		state.agent.RecordRecvProfileSize(size)
		state.agent.RecordRecvSize(size)
		c := in.c
		if c.peerId == "" {
			c.peerId = message.GetPeerId()
		}

		message.Alias = c.alias
		broadcast(state, c, in.tReceived, message, false)
	case in := <-state.flowStatusQueue:
		message := &FlowStatusMessage{}
		if err := proto.Unmarshal(in.bytes, message); err != nil {
			log.Println("Failed to decode flow status message")
			return
		}

		size := len(in.bytes)
		state.agent.RecordRecvFlowStatusSize(size)
		state.agent.RecordRecvSize(size)

		flowStatus := message.GetFlowStatus()
		if flowStatus != FlowStatus_UNKNOWN_STATUS {
			in.c.flowStatus = flowStatus
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

	_, ms := now()
	msg := &ClientDisconnectedFromServerMessage{
		Type:  MessageType_CLIENT_DISCONNECTED_FROM_SERVER,
		Time:  ms,
		Alias: c.alias,
	}

	broadcast(state, c, time.Now(), msg, true)
}

func processPositionMessage(state *worldCommunicationState, in *inMessage) {
	c := in.c
	if state.transient.positionMessage == nil {
		state.transient.positionMessage = &PositionMessage{}
	}
	message := state.transient.positionMessage
	if err := proto.Unmarshal(in.bytes, message); err != nil {
		log.Println("Failed to decode position message")
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
	broadcast(state, c, in.tReceived, message, false)
}

func broadcast(state *worldCommunicationState, from *client, tReceived time.Time, msg proto.Message, isSystem bool) {
	if from.position == nil {
		return
	}

	t := time.Now()

	bytes, err := proto.Marshal(msg)
	if err != nil {
		log.Println("encode message failed", err)
		return
	}

	out := &outMessage{
		tReceived: tReceived,
		bytes:     bytes,
		isSystem:  isSystem,
	}

	commArea := makeCommArea(from.position.parcel)

	for c := range state.clients {
		if c == from {
			continue
		}

		if c.position == nil {
			continue
		}

		if c.flowStatus != FlowStatus_OPEN {
			continue
		}

		if !commArea.contains(c.position.parcel) {
			continue
		}

		c.send <- out
	}

	state.agent.RecordBroadcastDuration(time.Now().Sub(t))
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
		}
	}
}

func write(state *worldCommunicationState, c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	recordEndToEndDuration := func(out *outMessage) {
		if !out.isSystem {
			tReceived := out.tReceived
			d := time.Now().Sub(tReceived)
			state.agent.RecordEndToEndDuration(d)
		}
	}

	for {
		select {
		case out, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteCloseMessage()
				return
			}
			err := c.conn.WriteMessage(out.bytes)
			if err != nil {
				log.Println("error writing message", err)
				return
			}

			state.agent.RecordSentSize(len(out.bytes))
			recordEndToEndDuration(out)

			n := len(c.send)
			for i := 0; i < n; i++ {
				out = <-c.send
				err := c.conn.WriteMessage(out.bytes)
				if err != nil {
					log.Println("error writing message", err)
					return
				}
				state.agent.RecordSentSize(len(out.bytes))
				recordEndToEndDuration(out)
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
