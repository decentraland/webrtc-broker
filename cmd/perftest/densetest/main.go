package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/decentraland/webrtc-broker/pkg/simulation"
	"github.com/rs/zerolog"
)

func newLogger(name string) zerolog.Logger {
	return zerolog.New(os.Stdout).Level(zerolog.InfoLevel).With().Timestamp().Str("name", name).Logger()
}

func main() {
	var coordinatorURL string

	var nBots int

	var spawnObserver bool

	flag.StringVar(&coordinatorURL, "coordinatorURL", "ws://localhost:9090", "")
	flag.IntVar(&nBots, "n", 50, "")
	flag.BoolVar(&spawnObserver, "observer", false, "")
	flag.Parse()

	fmt.Println("starting test: ", coordinatorURL)

	for i := 0; i < nBots; i++ {
		log := newLogger(fmt.Sprintf("client-%d", i))

		go simulation.StartBot(simulation.BotOptions{
			CoordinatorURL: coordinatorURL,
			Topic:          "testtopic",
			Subscription:   map[string]bool{"testtopic": true},
			TrackStats:     false,
			Log:            log,
		})
	}

	if spawnObserver {
		log := newLogger("observer")

		simulation.StartBot(simulation.BotOptions{
			CoordinatorURL: coordinatorURL,
			Topic:          "testtopic",
			Subscription:   map[string]bool{"testtopic": true},
			TrackStats:     true,
			Log:            log,
		})
	} else {
		select {}
	}
}
