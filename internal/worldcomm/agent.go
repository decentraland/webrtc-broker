package worldcomm

import (
	"time"

	"github.com/decentraland/communications-server-go/internal/agent"
)

type WorldCommAgent struct {
	Agent agent.IAgent
}

func (agent *WorldCommAgent) recordMetric(metric string, value float64) {
	agent.Agent.RecordMetric(metric, value)
}

func (agent *WorldCommAgent) RecordSentToCoordinatorSize(size int) {
	agent.recordMetric("SentToCoordinatorSize[bytes]", float64(size))
}

func (agent *WorldCommAgent) RecordReceivedFromCoordinatorSize(size int) {
	agent.recordMetric("ReceivedFromCoordinatorSize[bytes]", float64(size))
}

func (agent *WorldCommAgent) RecordSentReliableToPeerSize(size int) {
	v := float64(size)
	agent.recordMetric("SentToPeerReliableSize[bytes]", v)
	agent.recordMetric("SentToPeerTotalSize[bytes]", v)
}

func (agent *WorldCommAgent) RecordReceivedReliableFromPeerSize(size int) {
	v := float64(size)
	agent.recordMetric("ReceivedFromPeerReliable[bytes]", v)
	agent.recordMetric("ReceivedFromPeerTotalSize[bytes]", v)
}

func (agent *WorldCommAgent) RecordSentUnreliableToPeerSize(size int) {
	v := float64(size)
	agent.recordMetric("SentToPeerUnreliableSize[bytes]", v)
	agent.recordMetric("SentToPeerTotalSize[bytes]", v)
}

func (agent *WorldCommAgent) RecordReceivedUnreliableFromPeerSize(size int) {
	v := float64(size)
	agent.recordMetric("ReceivedFromPeerUnreliable[bytes]", v)
	agent.recordMetric("ReceivedFromPeerTotalSize[bytes]", v)
}

func (agent *WorldCommAgent) RecordTotalPeerConnections(total int) {
	agent.recordMetric("TotalPeerConnections[connections]", float64(total))
}

func (agent *WorldCommAgent) RecordTotalTopicSubscriptions(total int) {
	agent.recordMetric("TotalTopicSubscriptions[topics]", float64(total))
}

func (agent *WorldCommAgent) RecordInflight(duration time.Duration) {
	agent.recordMetric("TimeInFlight[ns]", float64(duration.Nanoseconds()))
}

func (agent *WorldCommAgent) RecordQueues(state *WorldCommunicationState) {
	agent.recordMetric("topicQ[messages]", float64(len(state.topicQueue)))
	agent.recordMetric("connectQ[messages]", float64(len(state.connectQueue)))
	agent.recordMetric("webrtcControlQ[messages]", float64(len(state.webRtcControlQueue)))
	agent.recordMetric("messagesQ[messages]", float64(len(state.messagesQueue)))
	agent.recordMetric("unregisterQ[messages]", float64(len(state.unregisterQueue)))
}
