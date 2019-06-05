package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/coordinator"
	"github.com/sirupsen/logrus"
)

func main() {
	host := flag.String("host", "localhost", "")
	port := flag.Int("port", 9090, "")
	flag.Parse()

	log := logrus.New()
	defer logging.LogPanic()

	auth := authentication.NoopAuthenticator{}

	config := coordinator.Config{
		Auth: &auth,
		Log:  log,
	}
	state := coordinator.MakeState(&config)

	go coordinator.Start(state)

	mux := http.NewServeMux()
	coordinator.Register(state, mux)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Info("starting coordinator ", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
