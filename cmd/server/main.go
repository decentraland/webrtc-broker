package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/decentraland/communications-server-go/internal/agent"
	"github.com/decentraland/communications-server-go/internal/authentication"
	"github.com/decentraland/communications-server-go/internal/logging"
	"github.com/decentraland/communications-server-go/internal/utils"
	"github.com/decentraland/communications-server-go/internal/webrtc"
	"github.com/decentraland/communications-server-go/internal/worldcomm"
	protocol "github.com/decentraland/communications-server-go/pkg/protocol"

	_ "net/http/pprof"
)

func main() {
	config := worldcomm.Config{}

	flag.StringVar(&config.CoordinatorUrl, "coordinatorUrl", "ws://localhost:9090", "")
	flag.StringVar(&config.AuthMethod, "authMethod", "secret", "noop")

	version := flag.String("version", "UNKNOWN", "")
	newrelicApiKey := flag.String("newrelicKey", "", "")
	appName := flag.String("appName", "dcl-comm-server", "")
	reportCaller := flag.Bool("reportCaller", false, "")
	logLevel := flag.String("logLevel", "debug", "")
	profilerPort := flag.Int("profilerPort", -1, "If not provided, profiler won't be enabled")
	noopAuthEnabled := flag.Bool("noopAuthEnabled", false, "")

	flag.Parse()

	logging.SetReportCaller(*reportCaller)
	err := logging.SetLevel(*logLevel)
	log := logging.New()

	if err != nil {
		log.Error("error setting log level")
		return
	}

	defer logging.LogPanic()

	agent, err := agent.Make(*appName, *newrelicApiKey)
	if err != nil {
		log.Fatal("Cannot initialize new relic: ", err)
	}

	if *profilerPort != -1 {
		go func() {
			addr := fmt.Sprintf("localhost:%d", *profilerPort)
			log.Info("Starting profiler at ", addr)
			log.Debug(http.ListenAndServe(addr, nil))
		}()
	}

	auth := authentication.Make()
	if *noopAuthEnabled {
		auth.AddOrUpdateAuthenticator("noop", &authentication.NoopAuthenticator{})
	}

	config.Services = worldcomm.Services{
		Auth:       auth,
		Marshaller: &protocol.Marshaller{},
		Log:        logging.New(),
		WebRtc:     webrtc.MakeWebRtc(),
		Agent:      &worldcomm.WorldCommAgent{Agent: agent},
		Zipper:     &utils.GzipCompression{},
	}

	config.CoordinatorUrl = fmt.Sprintf("%s/discover", config.CoordinatorUrl)
	worldCommState := worldcomm.MakeState(config)

	log.Info("starting communication server node, - version:", *version)

	if err := worldcomm.ConnectCoordinator(&worldCommState); err != nil {
		log.Fatal("connect coordinator failure ", err)
	}

	go worldcomm.ProcessMessagesQueue(&worldCommState)
	worldcomm.Process(&worldCommState)
}
