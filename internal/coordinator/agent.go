package coordinator

import (
	"github.com/decentraland/communications-server-go/internal/agent"
)

type coordinatorAgent struct {
	agent agent.IAgent
}

func (agent *coordinatorAgent) recordMetric(metric string, value float64) {
	agent.agent.RecordMetric(metric, value)
}

func (agent *coordinatorAgent) RecordSentSize(size int) {
	agent.recordMetric("SentSize[bytes]", float64(size))
}

func (agent *coordinatorAgent) RecordReceivedSize(size int) {
	agent.recordMetric("ReceivedSize[bytes]", float64(size))
}

func (agent *coordinatorAgent) RecordTotalClientConnections(total int) {
	agent.recordMetric("TotalClientConnections[connections]", float64(total))
	agent.recordMetric("TotalConnections[connections]", float64(total))
}

func (agent *coordinatorAgent) RecordTotalServerConnections(total int) {
	agent.recordMetric("TotalServerConnections[connections]", float64(total))
	agent.recordMetric("TotalConnections[connections]", float64(total))
}
