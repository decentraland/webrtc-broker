package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/decentraland/webrtc-broker/internal/logging"
	"github.com/decentraland/webrtc-broker/pkg/authentication"
	"github.com/decentraland/webrtc-broker/pkg/commserver"

	"github.com/rs/zerolog"

	_ "net/http/pprof"
)

type totals struct {
	bytesReceivedByDC uint64
	bytesSentByDC     uint64

	bytesReceivedBySCTP uint64
	bytesSentBySCTP     uint64

	bytesReceivedByICE uint64
	bytesSentByICE     uint64

	messagesSentByDC     uint64
	messagesReceivedByDC uint64
}

type reporter struct {
	lastT totals
	log   zerolog.Logger
}

func (r *reporter) Report(stats commserver.Stats) {
	t := totals{}

	stateCount := make(map[commserver.ICEConnectionState]uint32)
	localCandidateTypeCount := make(map[commserver.ICECandidateType]uint32)
	remoteCandidateTypeCount := make(map[commserver.ICECandidateType]uint32)

	for _, peerStats := range stats.Peers {
		t.bytesSentByDC += peerStats.ReliableBytesSent + peerStats.UnreliableBytesSent
		t.bytesReceivedByDC += peerStats.ReliableBytesReceived + peerStats.UnreliableBytesReceived
		t.messagesSentByDC += uint64(peerStats.ReliableMessagesSent) + uint64(peerStats.UnreliableMessagesSent)
		t.messagesReceivedByDC += uint64(peerStats.ReliableMessagesReceived) + uint64(peerStats.UnreliableMessagesReceived)

		t.bytesSentBySCTP += peerStats.SCTPTransportBytesSent
		t.bytesReceivedBySCTP += peerStats.SCTPTransportBytesReceived

		t.bytesSentByICE += peerStats.ICETransportBytesSent
		t.bytesReceivedByICE += peerStats.ICETransportBytesReceived

		stateCount[peerStats.State]++

		if peerStats.Nomination {
			localCandidateTypeCount[peerStats.LocalCandidateType]++
			remoteCandidateTypeCount[peerStats.LocalCandidateType]++
		}
	}

	messagesSent := t.messagesSentByDC - r.lastT.messagesSentByDC
	bytesSent := t.bytesSentByDC - r.lastT.bytesSentByDC
	iceBytesSent := t.bytesSentByICE - r.lastT.bytesSentByICE
	sctpBytesSent := t.bytesSentBySCTP - r.lastT.bytesSentBySCTP

	messagesReceived := t.messagesReceivedByDC - r.lastT.messagesReceivedByDC
	bytesReceived := t.bytesReceivedByDC - r.lastT.bytesReceivedByDC
	iceBytesReceived := t.bytesReceivedByICE - r.lastT.bytesReceivedByICE
	sctpBytesReceived := t.bytesReceivedBySCTP - r.lastT.bytesReceivedBySCTP

	r.log.Info().Str("log_type", "report").
		Uint64("messages sent per second [DC]", messagesSent/10).
		Uint64("bytes sent per second [DC]", bytesSent/10).
		Uint64("bytes sent per second [ICE]", iceBytesSent/10).
		Uint64("bytes sent per second [SCTP]", sctpBytesSent/10).
		Uint64("messages received per second [DC]", messagesReceived/10).
		Uint64("bytes received per second [DC]", bytesReceived/10).
		Uint64("bytes received per second [ICE]", iceBytesReceived/10).
		Uint64("bytes received per second [SCTP]", sctpBytesReceived/10).
		Int("peer_count", len(stats.Peers)).
		Int("topic_count", stats.TopicCount).
		Msg("")

	r.lastT = t
}

func main() {
	log := logging.New().Level(zerolog.DebugLevel)
	defer logging.LogPanic(log)

	reporter := reporter{log: log}

	auth := authentication.NoopAuthenticator{}
	config := commserver.Config{
		Auth: &auth,
		Log:  &log,
		ICEServers: []commserver.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
		ReportPeriod: 10 * time.Second,
		Reporter:     reporter.Report,
	}

	flag.StringVar(&config.CoordinatorURL, "coordinatorURL", "ws://localhost:9090", "")
	profilerPort := flag.Int("profilerPort", -1, "If not provided, profiler won't be enabled")

	flag.Parse()

	if *profilerPort != -1 {
		go func() {
			addr := fmt.Sprintf("0.0.0.0:%d", *profilerPort)
			log.Info().Msgf("Starting profiler at %s", addr)
			log.Debug().Err(http.ListenAndServe(addr, nil))
		}()
	}

	state, err := commserver.MakeState(&config)

	if err != nil {
		log.Fatal().Err(err)
	}

	log.Info().Msg("starting communication server node")

	if err := commserver.ConnectCoordinator(state); err != nil {
		log.Fatal().Err(err).Msg("connect coordinator failure")
	}

	go commserver.ProcessMessagesQueue(state)
	commserver.Process(state)
}
