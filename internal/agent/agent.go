package agent

import (
	newrelic "github.com/newrelic/go-agent"
	"log"
	"time"
)

type IAgent interface {
	RecordTotalConnections(total int)
	RecordEndToEndDuration(d time.Duration)
	RecordBroadcastDuration(d time.Duration)
	RecordSentSize(size int)
	RecordRecvSize(size int)
	RecordRecvPositionSize(size int)
	RecordRecvChatSize(size int)
	RecordRecvFlowStatusSize(size int)
	RecordRecvProfileSize(size int)
	RecordRecvWebRtcAnswerSize(size int)
}

type newrelicAgent struct {
	app newrelic.Application
}

func (agent *newrelicAgent) RecordTotalConnections(total int) {
	agent.app.RecordCustomMetric("TotalConnections[connections]", float64(total))
}

func (agent *newrelicAgent) RecordEndToEndDuration(d time.Duration) {
	t := float64(d.Nanoseconds() / int64(time.Millisecond))
	agent.app.RecordCustomMetric("EndToEndDuration[milliseconds|call]", t)
}

func (agent *newrelicAgent) RecordBroadcastDuration(d time.Duration) {
	t := float64(d.Nanoseconds() / int64(time.Millisecond))
	agent.app.RecordCustomMetric("BroadcastDuration[milliseconds|call]", t)
}

func (agent *newrelicAgent) RecordSentSize(size int) {
	agent.app.RecordCustomMetric("SentSize[bytes]", float64(size))
}

func (agent *newrelicAgent) RecordRecvSize(size int) {
	agent.app.RecordCustomMetric("RecvSize[bytes]", float64(size))
}

func (agent *newrelicAgent) RecordRecvPositionSize(size int) {
	agent.app.RecordCustomMetric("RecvPositionSize[bytes]", float64(size))
}

func (agent *newrelicAgent) RecordRecvChatSize(size int) {
	agent.app.RecordCustomMetric("RecvChatSize[bytes]", float64(size))
}

func (agent *newrelicAgent) RecordRecvFlowStatusSize(size int) {
	agent.app.RecordCustomMetric("RecvFlowStatusSize[bytes]", float64(size))
}

func (agent *newrelicAgent) RecordRecvProfileSize(size int) {
	agent.app.RecordCustomMetric("RecvProfileSize[bytes]", float64(size))
}

func (agent *newrelicAgent) RecordRecvWebRtcAnswerSize(size int) {
	agent.app.RecordCustomMetric("RecvWebRtcAnswerSize[bytes]", float64(size))
}

type noopAgent struct{}

func (_ *noopAgent) RecordTotalConnections(total int)        {}
func (_ *noopAgent) RecordEndToEndDuration(d time.Duration)  {}
func (_ *noopAgent) RecordBroadcastDuration(d time.Duration) {}
func (_ *noopAgent) RecordSentSize(size int)                 {}
func (_ *noopAgent) RecordRecvSize(size int)                 {}
func (_ *noopAgent) RecordRecvPositionSize(size int)         {}
func (_ *noopAgent) RecordRecvChatSize(size int)             {}
func (_ *noopAgent) RecordRecvFlowStatusSize(size int)       {}
func (_ *noopAgent) RecordRecvProfileSize(size int)          {}
func (_ *noopAgent) RecordRecvWebRtcAnswerSize(size int)     {}

func Make(appName string, newrelicApiKey string) (IAgent, error) {
	if newrelicApiKey == "" {
		return &noopAgent{}, nil
	}

	config := newrelic.NewConfig(appName, newrelicApiKey)
	app, err := newrelic.NewApplication(config)

	if err != nil {
		return nil, err
	}

	log.Println("newrelic loaded for", appName)

	return &newrelicAgent{app: app}, nil
}
