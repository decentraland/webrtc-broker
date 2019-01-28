package agent

import (
	"log"

	newrelic "github.com/newrelic/go-agent"
)

type IAgent interface {
	RecordMetric(string, float64)
}

type newrelicAgent struct {
	app newrelic.Application
}

func (agent *newrelicAgent) RecordMetric(metric string, value float64) {
	agent.app.RecordCustomMetric(metric, value)
}

type noopAgent struct{}

func (*noopAgent) RecordMetric(metric string, value float64) {}

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
