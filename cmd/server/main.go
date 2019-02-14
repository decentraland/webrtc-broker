package main

import (
	"flag"
	"fmt"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/worldcomm"
	"github.com/pions/webrtc"
)

func main() {
	coordinatorUrl := flag.String("coordinatorUrl", "ws://localhost:9090", "")
	version := flag.String("version", "UNKNOWN", "")
	newrelicApiKey := flag.String("newrelicKey", "", "")
	appName := flag.String("appName", "dcl-comm-server", "")
	reportCaller := flag.Bool("reportCaller", false, "")
	logLevel := flag.String("logLevel", "debug", "")
	authMethod := flag.String("authMethod", "secret", "") //TODO set a proper default
	noopAuthEnabled := flag.Bool("noopAuthEnabled", false, "")
	flag.Parse()

	logging.SetReportCaller(*reportCaller)
	err := logging.SetLevel(*logLevel)
	log := logging.New()

	if err != nil {
		log.Error("error setting log level")
		return
	}

	agent, err := agent.Make(*appName, *newrelicApiKey)
	if err != nil {
		log.Fatal("Cannot initialize new relic: ", err)
	}

	u := fmt.Sprintf("%s/discover", *coordinatorUrl)
	s := worldcomm.MakeState(agent, *authMethod, u)

	if *noopAuthEnabled {
		s.Auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})
	}
	log.Info("starting communication server node, - version:", *version)

	webrtc.DetachDataChannels()
	if err := worldcomm.ConnectCoordinator(&s); err != nil {
		log.Fatal("connect coordinator failure ", err)
	}

	worldcomm.Process(&s)
}
