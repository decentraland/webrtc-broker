package main

import (
	"flag"
	"fmt"
	"github.com/decentraland/communications-server-go/worldcomm"
	newrelic "github.com/newrelic/go-agent"
	"log"
	"net/http"
)

const (
	worldCommunicationEnabled = true
)

func initWorldCommunication() {
	state := worldcomm.MakeState()
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

	if *newrelicApiKey != "" {
		config := newrelic.NewConfig("dcl-comm-server", *newrelicApiKey)
		_, err := newrelic.NewApplication(config)

		if err != nil {
			log.Fatal("Cannot initialize new relic: ", err)
		}
	}

	initWorldCommunication()

	log.Println("OK", addr)

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
