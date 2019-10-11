package broker

import (
	"time"

	pion "github.com/pion/webrtc/v2"
)

// StatsSummary ...
type StatsSummary struct {
	From time.Time
	To   time.Time

	StateCount               map[pion.ICEConnectionState]uint32
	LocalCandidateTypeCount  map[pion.ICECandidateType]uint32
	RemoteCandidateTypeCount map[pion.ICECandidateType]uint32

	MessagesSentByDC, BytesSentByDC, BytesSentByICE, BytesSentBySCTP                 uint64
	MessagesReceivedByDC, BytesReceivedByDC, BytesReceivedByICE, BytesReceivedBySCTP uint64
}

// StatsSummaryGenerator ...
type StatsSummaryGenerator struct {
	lastStats Stats
}

// NewStatsSummaryGenerator creates a StatsSummaryGenerator
func NewStatsSummaryGenerator() StatsSummaryGenerator {
	return StatsSummaryGenerator{
		lastStats: Stats{Peers: make(map[uint64]PeerStats)},
	}
}

// Generate generates a new StatsSummary comparing the new stats with the last processed one
func (g *StatsSummaryGenerator) Generate(stats Stats) StatsSummary {
	summary := StatsSummary{
		From:                     g.lastStats.Time,
		To:                       stats.Time,
		StateCount:               make(map[pion.ICEConnectionState]uint32),
		LocalCandidateTypeCount:  make(map[pion.ICECandidateType]uint32),
		RemoteCandidateTypeCount: make(map[pion.ICECandidateType]uint32),
	}

	for alias, pStats := range stats.Peers {
		pLastStats := g.lastStats.Peers[alias]

		summary.StateCount[pStats.State]++

		if pStats.Nomination {
			summary.LocalCandidateTypeCount[pStats.LocalCandidateType]++
			summary.RemoteCandidateTypeCount[pStats.LocalCandidateType]++
		}

		summary.MessagesSentByDC += g.getMessagesSentByDC(pStats) - g.getMessagesSentByDC(pLastStats)
		summary.BytesSentByDC += g.getBytesSentByDC(pStats) - g.getBytesSentByDC(pLastStats)
		summary.BytesSentByICE += pStats.ICETransportBytesSent - pLastStats.ICETransportBytesSent
		summary.BytesSentBySCTP += pStats.SCTPTransportBytesSent - pLastStats.SCTPTransportBytesSent

		summary.MessagesReceivedByDC += g.getMessagesReceivedByDC(pStats) - g.getMessagesReceivedByDC(pLastStats)
		summary.BytesReceivedByDC += g.getBytesReceivedByDC(pStats) - g.getBytesReceivedByDC(pLastStats)
		summary.BytesReceivedByICE += pStats.ICETransportBytesReceived - pLastStats.ICETransportBytesReceived
		summary.BytesReceivedBySCTP += pStats.SCTPTransportBytesReceived - pLastStats.SCTPTransportBytesReceived
	}

	g.lastStats = stats

	return summary
}

func (g *StatsSummaryGenerator) getBytesSentByDC(s PeerStats) uint64 {
	return s.ReliableBytesSent + s.UnreliableBytesSent
}

func (g *StatsSummaryGenerator) getBytesReceivedByDC(s PeerStats) uint64 {
	return s.ReliableBytesReceived + s.UnreliableBytesReceived
}

func (g *StatsSummaryGenerator) getMessagesSentByDC(s PeerStats) uint64 {
	return uint64(s.ReliableMessagesSent) + uint64(s.UnreliableMessagesSent)
}

func (g *StatsSummaryGenerator) getMessagesReceivedByDC(s PeerStats) uint64 {
	return uint64(s.ReliableMessagesReceived) + uint64(s.UnreliableMessagesReceived)
}
