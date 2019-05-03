package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/commserver"
	"github.com/sirupsen/logrus"

	_ "net/http/pprof"
)

func main() {
	log := logrus.New()
	defer logging.LogPanic()

	auth := authentication.Make()
	config := commserver.Config{
		Auth: auth,
		Log:  log,
		ICEServers: []commserver.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	flag.StringVar(&config.CoordinatorURL, "coordinatorUrl", "ws://localhost:9090", "")
	flag.StringVar(&config.AuthMethod, "authMethod", "secret", "noop")

	profilerPort := flag.Int("profilerPort", -1, "If not provided, profiler won't be enabled")
	noopAuthEnabled := flag.Bool("noopAuthEnabled", false, "")

	flag.Parse()

	if *profilerPort != -1 {
		go func() {
			addr := fmt.Sprintf("localhost:%d", *profilerPort)
			log.Info("Starting profiler at ", addr)
			log.Debug(http.ListenAndServe(addr, nil))
		}()
	}

	if *noopAuthEnabled {
		auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})
	}

	config.CoordinatorURL = fmt.Sprintf("%s/discover", config.CoordinatorURL)
	state, err := commserver.MakeState(&config)

	if err != nil {
		log.Fatal(err)
	}

	log.Info("starting communication server node")

	if err := commserver.ConnectCoordinator(state); err != nil {
		log.Fatal("connect coordinator failure ", err)
	}

	go commserver.ProcessMessagesQueue(state)
	commserver.Process(state)
}
