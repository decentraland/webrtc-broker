package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/coordinator"
	"github.com/decentraland/communications-server-go/internal/logging"
)

func main() {
	host := flag.String("host", "localhost", "")
	port := flag.Int("port", 9090, "")
	version := flag.String("version", "UNKNOWN", "")
	newrelicApiKey := flag.String("newrelicKey", "", "")
	appName := flag.String("appName", "dcl-comm-coordinator", "")
	reportCaller := flag.Bool("reportCaller", false, "")
	logLevel := flag.String("logLevel", "debug", "")
	noopAuthEnabled := flag.Bool("noopAuthEnabled", false, "")
	flag.Parse()

	logging.SetReportCaller(*reportCaller)
	err := logging.SetLevel(*logLevel)
	log := logging.New()

	if err != nil {
		log.Error("error setting log level")
		return
	}
	addr := fmt.Sprintf("%s:%d", *host, *port)

	agent, err := agent.Make(*appName, *newrelicApiKey)
	if err != nil {
		log.Fatal("Cannot initialize new relic: ", err)
	}

	selector := coordinator.MakeRandomServerSelector()
	c := coordinator.MakeState(agent, selector)
	if *noopAuthEnabled {
		c.Auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})
	}

	go coordinator.Process(&c)

	http.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeDiscoverRequest(&c, w, r)

		if err != nil {
			log.WithError(err).Error("socket connect error (discovery)")
			return
		}

		coordinator.ConnectCommServer(&c, ws)
	})

	http.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		ws, err := coordinator.UpgradeConnectRequest(&c, w, r)

		if err != nil {
			log.WithError(err).Error("socket connect error (client)")
			return
		}

		coordinator.ConnectClient(&c, ws)
	})

	log.Info("starting coordinator", addr, "- version:", *version)
	log.Fatal(http.ListenAndServe(addr, nil))
}
