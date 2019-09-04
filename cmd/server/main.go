package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/commserver"

	"github.com/rs/zerolog"

	_ "net/http/pprof"
)

func main() {
	log := logging.New().Level(zerolog.DebugLevel)
	defer logging.LogPanic(log)

	auth := authentication.NoopAuthenticator{}
	config := commserver.Config{
		Auth: &auth,
		Log:  &log,
		ICEServers: []commserver.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	flag.StringVar(&config.CoordinatorURL, "coordinatorUrl", "ws://localhost:9090", "")
	profilerPort := flag.Int("profilerPort", -1, "If not provided, profiler won't be enabled")

	flag.Parse()

	if *profilerPort != -1 {
		go func() {
			addr := fmt.Sprintf("localhost:%d", *profilerPort)
			log.Info().Msgf("Starting profiler at %s", addr)
			log.Debug().Err(http.ListenAndServe(addr, nil))
		}()
	}

	state, err := commserver.MakeState(&config)

	if err != nil {
		log.Fatal().Err(err)
	}

	log.Info().Msg("starting communication server node")

	if err := commserver.ConnectCoordinator(state); err != nil {
		log.Fatal().Err(err).Msg("connect coordinator failure")
	}

	go commserver.ProcessMessagesQueue(state)
	commserver.Process(state)
}
