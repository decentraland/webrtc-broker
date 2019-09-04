package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/coordinator"
)

func main() {
	host := flag.String("host", "localhost", "")
	port := flag.Int("port", 9090, "")
	flag.Parse()

	log := logging.New()
	defer logging.LogPanic(log)

	config := coordinator.Config{
		Auth: &authentication.NoopAuthenticator{},
		Log:  &log,
	}
	state := coordinator.MakeState(&config)

	go coordinator.Start(state)

	mux := http.NewServeMux()
	coordinator.Register(state, mux)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Info().Msgf("starting coordinator at %s", addr)
	log.Fatal().Err(http.ListenAndServe(addr, mux))
}
