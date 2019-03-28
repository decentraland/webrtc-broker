package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"

	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/simulation"
)

type V3 = simulation.V3

func main() {
	addrP := flag.String("worldUrl", "ws://localhost:9090/connect", "")
	centerXP := flag.Int("centerX", 0, "")
	centerYP := flag.Int("centerY", 0, "")
	radiusP := flag.Int("radius", 3, "radius (in parcels) from the center")
	subscribeP := flag.Bool("subscribe", false, "subscribe to the position and profile topics of the comm area")
	nBotsP := flag.Int("n", 5, "number of bots")
	authMethodP := flag.String("authMethod", "noop", "")
	profilerPort := flag.Int("profilerPort", -1, "If not provided, profiler won't be enabled")
	trackStats := flag.Bool("trackStats", false, "")

	flag.Parse()

	log.Println("running random simulation")

	auth := authentication.Make()
	auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})

	addr := *addrP
	centerX := *centerXP
	centerY := *centerYP
	radius := *radiusP
	subscribe := *subscribeP
	authMethod := *authMethodP

	if *profilerPort != -1 {
		go func() {
			addr := fmt.Sprintf("localhost:%d", *profilerPort)
			log.Println("Starting profiler at", addr)
			log.Println(http.ListenAndServe(addr, nil))
		}()
	}

	for i := 0; i < *nBotsP; i += 1 {
		var checkpoints [6]V3

		for i := 0; i < len(checkpoints); i += 1 {
			p := &checkpoints[i]

			p.X = float64(centerX + rand.Intn(10)*radius*2 - radius)
			p.Y = 1.6
			p.Z = float64(centerY + rand.Intn(10)*radius*2 - radius)
		}

		opts := simulation.BotOptions{
			Auth:                      auth,
			AuthMethod:                authMethod,
			Checkpoints:               checkpoints[:],
			DurationMs:                10000,
			SubscribeToPositionTopics: subscribe,
			TrackStats:                *trackStats,
		}

		go simulation.StartBot(addr, opts)
	}

	select {}
}
