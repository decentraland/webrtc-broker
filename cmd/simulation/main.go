package main

import (
	"github.com/decentraland/communications-server-go/internal/simulation"
	"log"
	"flag"
	"math/rand"
)

type V3 = simulation.V3

func startRndBot(addr string, centerX int, centerY int, radius int) {
	var checkpoints [6]V3

	for i:=0; i < len(checkpoints); i += 1 {
		p := &checkpoints[i]

		p.X = float64(centerX + rand.Intn(10) * radius * 2 - radius)
		p.Y = 1.6
		p.Z = float64(centerY + rand.Intn(10) * radius * 2 - radius)
	}

	opts := simulation.BotOptions{
		CommServerUrl: addr,
		Checkpoints: checkpoints[:],
		DurationMs: 10000,
	}

	_, err := simulation.StartBot(opts)

	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	addr := flag.String("commServer", "ws://localhost:9090/connector", "")
	centerX := flag.Int("centerX", 0 , "")
	centerY := flag.Int("centerY", 0, "")
	radius := flag.Int("radius", 3, "radius (in parcels) from the center")
	nBots := flag.Int("n", 5, "number of bots")

	flag.Parse()

	log.Println("running random simulation")

	for i := 1; i <= *nBots; i += 1 {
		startRndBot(*addr, *centerX, *centerY, *radius)
	}

	select {}
}
