package main

import (
	"flag"
	"fmt"
	"net/http"

	_ "net/http/pprof" //nolint:gosec
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/broker"
	protocol "github.com/decentraland/webrtc-broker/pkg/protocol"
	pion "github.com/pion/webrtc/v2"

	"github.com/rs/zerolog"
)

const (
	profilerPort = 9082
)

func main() {
	log := logging.New().Level(zerolog.DebugLevel)
	defer logging.LogPanic(log)

	auth := authentication.NoopAuthenticator{}
	config := broker.Config{
		Auth: &auth,
		Log:  &log,
		ICEServers: []pion.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
		Role: protocol.Role_COMMUNICATION_SERVER,
	}

	flag.StringVar(&config.CoordinatorURL, "coordinatorURL", "ws://localhost:9090", "")
	flag.Parse()

	go func() {
		addr := fmt.Sprintf("0.0.0.0:%d", profilerPort)
		log.Info().Msgf("Starting profiler at %s", addr)
		log.Debug().Err(http.ListenAndServe(addr, nil))
	}()

	b, err := broker.NewBroker(&config)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating new broker")
	}

	log.Info().Msg("starting communication server node")

	if err := b.Connect(); err != nil {
		log.Fatal().Err(err).Msg("error connecting to coordinator")
	}

	go b.ProcessSubscriptionChannel()

	go b.ProcessMessagesChannel()

	go b.ProcessControlMessages()

	seconds := uint64(10)
	reportTicker := time.NewTicker(time.Duration(seconds) * time.Second)
	summaryGenerator := broker.NewStatsSummaryGenerator()

	for {
		<-reportTicker.C

		stats := b.GetBrokerStats()
		summary := summaryGenerator.Generate(stats)

		messagesSent := summary.MessagesSentByDC / seconds
		bytesSent := summary.BytesSentByDC / seconds
		iceBytesSent := summary.BytesSentByICE / seconds
		sctpBytesSent := summary.BytesSentBySCTP / seconds

		messagesReceived := summary.MessagesReceivedByDC / seconds
		bytesReceived := summary.BytesReceivedByDC / seconds
		iceBytesReceived := summary.BytesReceivedByICE / seconds
		sctpBytesReceived := summary.BytesReceivedBySCTP / seconds

		log.Info().
			Uint64("messages sent per second [DC]", messagesSent).
			Uint64("bytes sent per second [DC]", bytesSent).
			Uint64("bytes sent per second [ICE]", iceBytesSent).
			Uint64("bytes sent per second [SCTP]", sctpBytesSent).
			Uint64("messages received per second [DC]", messagesReceived).
			Uint64("bytes received per second [DC]", bytesReceived).
			Uint64("bytes received per second [ICE]", iceBytesReceived).
			Uint64("bytes received per second [SCTP]", sctpBytesReceived).
			Int("peer_count", len(stats.Peers)).
			Int("topic_count", stats.TopicCount).
			Msg("")
	}
}
