package commserver

import (
	pion "github.com/pion/webrtc/v2"
)

// Stats expose comm server stats for reporting purposes
type Stats struct {
	Alias               uint64
	TopicCount          int
	TopicChSize         int
	ConnectChSize       int
	WebRtcControlChSize int
	MessagesChSize      int
	UnregisterChSize    int
	Peers               []PeerStats
}

// PeerStats expose peer stats
type PeerStats struct {
	Alias      uint64
	Identity   []byte
	State      ICEConnectionState
	TopicCount uint32

	Nomination          bool
	LocalCandidateType  ICECandidateType
	RemoteCandidateType ICECandidateType

	DataChannelsOpened    uint32
	DataChannelsClosed    uint32
	DataChannelsRequested uint32
	DataChannelsAccepted  uint32

	ReliableProtocol         string
	ReliableState            DataChannelState
	ReliableBytesSent        uint64
	ReliableBytesReceived    uint64
	ReliableMessagesSent     uint32
	ReliableMessagesReceived uint32
	ReliableBufferedAmount   uint64

	UnreliableProtocol         string
	UnreliableState            DataChannelState
	UnreliableBytesSent        uint64
	UnreliableBytesReceived    uint64
	UnreliableMessagesSent     uint32
	UnreliableMessagesReceived uint32
	UnreliableBufferedAmount   uint64

	ICETransportBytesSent     uint64
	ICETransportBytesReceived uint64

	SCTPTransportBytesSent     uint64
	SCTPTransportBytesReceived uint64
}

func report(state *State) {
	if state.reporter == nil {
		return
	}

	log := state.services.Log

	getCandidateStats := func(report pion.StatsReport, statsID string) (pion.ICECandidateStats, bool) {
		stats, ok := report[statsID]
		if !ok {
			return pion.ICECandidateStats{}, ok
		}

		candidateStats, ok := stats.(pion.ICECandidateStats)
		if !ok {
			log.Warn().Msgf("requested ice candidate type %s, but is not from type ICECandidateStats", statsID)
		}

		return candidateStats, ok
	}

	getTransportStats := func(report pion.StatsReport, statsID string) (pion.TransportStats, bool) {
		stats, ok := report[statsID]
		if !ok {
			return pion.TransportStats{}, ok
		}

		transportStats, ok := stats.(pion.TransportStats)
		if !ok {
			log.Warn().Msgf("requested transport stats type %s, but is not from type TransportStats", statsID)
		}

		return transportStats, ok
	}

	state.subscriptionsLock.RLock()
	topicCount := len(state.subscriptions)
	state.subscriptionsLock.RUnlock()

	serverStats := Stats{
		Alias:               state.alias,
		TopicCount:          topicCount,
		Peers:               make([]PeerStats, len(state.peers)),
		TopicChSize:         len(state.topicCh),
		ConnectChSize:       len(state.connectCh),
		WebRtcControlChSize: len(state.webRtcControlCh),
		MessagesChSize:      len(state.messagesCh),
		UnregisterChSize:    len(state.unregisterCh),
	}

	for i, p := range state.peers {
		report := state.services.WebRtc.getStats(p.conn)
		stats := PeerStats{
			Alias:      p.alias,
			Identity:   p.GetIdentity(),
			TopicCount: uint32(len(p.topics)),
			State:      p.conn.ICEConnectionState(),
		}

		if p.reliableDC != nil {
			stats.ReliableBufferedAmount = p.reliableDC.BufferedAmount()
		}

		if p.unreliableDC != nil {
			stats.UnreliableBufferedAmount = p.unreliableDC.BufferedAmount()
		}

		connStats, ok := report.GetConnectionStats(p.conn)
		if ok {
			stats.DataChannelsOpened = connStats.DataChannelsOpened
			stats.DataChannelsClosed = connStats.DataChannelsClosed
			stats.DataChannelsRequested = connStats.DataChannelsRequested
			stats.DataChannelsAccepted = connStats.DataChannelsAccepted
		}

		reliableStats, ok := report.GetDataChannelStats(p.reliableDC)
		if ok {
			stats.ReliableProtocol = reliableStats.Protocol
			stats.ReliableState = reliableStats.State
			stats.ReliableMessagesSent = reliableStats.MessagesSent
			stats.ReliableBytesSent = reliableStats.BytesSent
			stats.ReliableMessagesReceived = reliableStats.MessagesReceived
			stats.ReliableBytesReceived = reliableStats.BytesReceived
		}

		unreliableStats, ok := report.GetDataChannelStats(p.unreliableDC)
		if ok {
			stats.UnreliableProtocol = unreliableStats.Protocol
			stats.UnreliableState = unreliableStats.State
			stats.UnreliableMessagesSent = unreliableStats.MessagesSent
			stats.UnreliableBytesSent = unreliableStats.BytesSent
			stats.UnreliableMessagesReceived = unreliableStats.MessagesReceived
			stats.UnreliableBytesReceived = unreliableStats.BytesReceived
		}

		iceTransportStats, ok := getTransportStats(report, "iceTransport")
		if ok {
			stats.ICETransportBytesSent = iceTransportStats.BytesSent
			stats.ICETransportBytesReceived = iceTransportStats.BytesReceived
		}

		sctpTransportStats, ok := getTransportStats(report, "sctpTransport")
		if ok {
			stats.SCTPTransportBytesSent = sctpTransportStats.BytesSent
			stats.SCTPTransportBytesReceived = sctpTransportStats.BytesReceived
		}

		for _, v := range report {
			pairStats, ok := v.(pion.ICECandidatePairStats)

			if !ok || !pairStats.Nominated {
				continue
			}

			stats.Nomination = true

			localCandidateStats, ok := getCandidateStats(report, pairStats.LocalCandidateID)
			if ok {
				stats.LocalCandidateType = localCandidateStats.CandidateType
			}

			remoteCandidateStats, ok := getCandidateStats(report, pairStats.RemoteCandidateID)
			if ok {
				stats.RemoteCandidateType = remoteCandidateStats.CandidateType
			}
		}

		serverStats.Peers[i] = stats
	}

	state.reporter(serverStats)
}
