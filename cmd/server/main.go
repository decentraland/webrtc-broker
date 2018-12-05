package main

import (
	"flag"
	"fmt"
	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/worldcomm"
	"github.com/decentraland/communications-server-go/internal/ws"
	"log"
	"net/http"
)

func main() {
	host := flag.String("host", "localhost", "")
	version := flag.String("version", "UNKNOWN", "")
	port := flag.Int("port", 9090, "")
	newrelicApiKey := flag.String("newrelicKey", "", "")
	appName := flag.String("appName", "dcl-comm-server", "")
	flag.Parse()

	addr := fmt.Sprintf("%s:%d", *host, *port)

	agent, err := agent.Make(*appName, *newrelicApiKey)
	if err != nil {
		log.Fatal("Cannot initialize new relic: ", err)
	}

	upgrader := ws.MakeUpgrader()

	wc := worldcomm.Make(agent)
	go wc.Process()
	http.HandleFunc("/connector", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r)

		if err != nil {
			log.Println("socket connect error", err)
			return
		}

		wc.Connect(ws)
	})

	log.Println("starting server", addr, "- version:", *version)

	err = http.ListenAndServe(addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
