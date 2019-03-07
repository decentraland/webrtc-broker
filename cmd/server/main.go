package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/worldcomm"

	_ "net/http/pprof"
)

func main() {
	coordinatorUrl := flag.String("coordinatorUrl", "ws://localhost:9090", "")
	version := flag.String("version", "UNKNOWN", "")
	newrelicApiKey := flag.String("newrelicKey", "", "")
	appName := flag.String("appName", "dcl-comm-server", "")
	reportCaller := flag.Bool("reportCaller", false, "")
	logLevel := flag.String("logLevel", "debug", "")
	authMethod := flag.String("authMethod", "secret", "noop")
	profilerPort := flag.Int("profilerPort", -1, "If not provided, profiler won't be enabled")
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

	if *profilerPort != -1 {
		go func() {
			addr := fmt.Sprintf("localhost:%d", *profilerPort)
			log.Info("Starting profiler at ", addr)
			log.Debug(http.ListenAndServe(addr, nil))
		}()
	}

	u := fmt.Sprintf("%s/discover", *coordinatorUrl)
	worldCommState := worldcomm.MakeState(agent, *authMethod, u)

	if *noopAuthEnabled {
		worldCommState.Auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})
	}
	log.Info("starting communication server node, - version:", *version)

	if err := worldcomm.ConnectCoordinator(&worldCommState); err != nil {
		log.Fatal("connect coordinator failure ", err)
	}

	worldcomm.Process(&worldCommState)
}
