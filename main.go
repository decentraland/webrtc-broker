package main

import (
	"github.com/decentraland/communications-server-go/worldcomm"
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
	if worldCommunicationEnabled {
		initWorldCommunication()
	}
	addr := "localhost:9090" // TODO
	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
