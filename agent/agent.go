package agent

import (
	"log"
	newrelic "github.com/newrelic/go-agent"
)

type MetricsContext struct {
	app newrelic.Application
}

func Initialize(newrelicApiKey string) (MetricsContext, error) {
	context := MetricsContext{app: nil}
	if newrelicApiKey == "" {
		return context, nil
	}

	config := newrelic.NewConfig("dcl-comm-server", newrelicApiKey)
	app, err := newrelic.NewApplication(config)

	if err != nil {
		return context, err
	}

	context.app = app

	log.Println("newrelic loaded")

	return context, nil
}
