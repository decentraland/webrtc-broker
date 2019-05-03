package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/pkg/authentication"
	"github.com/decentraland/communications-server-go/pkg/coordinator"
	"github.com/sirupsen/logrus"
)

func main() {
	host := flag.String("host", "localhost", "")
	port := flag.Int("port", 9090, "")
	noopAuthEnabled := flag.Bool("noopAuthEnabled", false, "")
	flag.Parse()

	log := logrus.New()
	defer logging.LogPanic()

	auth := authentication.Make()
	if *noopAuthEnabled {
		auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})
	}

	config := coordinator.Config{
		ServerSelector: coordinator.MakeRandomServerSelector(),
		Auth:           auth,
		Log:            log,
	}
	state := coordinator.MakeState(&config)

	go coordinator.Process(state)

	mux := http.NewServeMux()
	coordinator.Register(state, mux)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Info("starting coordinator ", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
