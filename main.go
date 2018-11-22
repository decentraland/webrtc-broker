package main

import (
	"flag"
	"fmt"
	"github.com/decentraland/communications-server-go/worldcomm"
	"github.com/decentraland/communications-server-go/agent"
	"log"
	"net/http"
)

func initWorldCommunication(metricsContext agent.MetricsContext) {
	state := worldcomm.MakeState(metricsContext)
	go worldcomm.Process(state)
	http.HandleFunc("/connector", func(w http.ResponseWriter, r *http.Request) {
		worldcomm.Connect(state, w, r)
	})
}

func main() {
	host := flag.String("host", "localhost", "")
	port := flag.Int("port", 9090, "")
	newrelicApiKey := flag.String("newrelicKey", "", "")
	flag.Parse()

	addr := fmt.Sprintf("%s:%d", *host, *port)

	metricsContext, err := agent.Initialize(*newrelicApiKey)
	if err != nil {
		log.Fatal("Cannot initialize new relic: ", err)
	}

	initWorldCommunication(metricsContext)

	log.Println("starting server", addr)

	err = http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
