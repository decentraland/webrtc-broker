package main

import (
	"fmt"
	"math"
)

// SCTP spec https://tools.ietf.org/html/rfc2960

const (
	// bytes
	maxPayloadSize = 1200

	sctpHeaderSize = 12
	chunkDataSize  = 16
	dtlsDataSize   = 37
	tsnForwardSize = 20
)

type dataDescription struct {
	size            int
	amountPerSecond int
	retries         int
}

type description struct {
	in struct {
		connections int
		data        []dataDescription
	}

	out struct {
		connections int
		data        []dataDescription
	}
}

type result struct {
	packagesSentPerSecond     uint32
	packagesReceivedPerSecond uint32
	bytesSentPerSecond        uint64
	bytesReceivedPerSecond    uint64
}

// since this is an upper bound I'm assuming no aggregation
func upperBound(desc description) result {
	r := result{}

	for _, dd := range desc.in.data {
		chunks := int(math.Ceil(float64(dd.size) / float64(maxPayloadSize)))

		// TODO: the chunk calculation is probably off, even with more than 1 chunk we only count it as 1 package
		received := (1 + dd.retries) * dd.amountPerSecond
		bytes := dd.size + sctpHeaderSize + chunks*chunkDataSize + dtlsDataSize

		r.packagesReceivedPerSecond += uint32(desc.in.connections * received)
		r.bytesReceivedPerSecond += uint64(desc.in.connections * received * bytes)
	}

	for _, dd := range desc.out.data {
		chunks := int(math.Ceil(float64(dd.size) / float64(maxPayloadSize)))

		// TODO: the chunk calculation is probably off, even with more than 1 chunk we only count it as 1 package
		sent := (1 + dd.retries) * dd.amountPerSecond
		bytes := dd.size + sctpHeaderSize + chunks*chunkDataSize + dtlsDataSize

		r.packagesSentPerSecond += uint32(desc.out.connections * sent)
		r.bytesSentPerSecond += uint64(desc.out.connections * sent * bytes)
	}

	// TsnForward chunk is generated as soon as the specified number of retransmission
	// or the specified time has passed since the first transmission
	// Assumme we have to generate a tsn forward for every package sent
	r.bytesSentPerSecond += uint64(r.packagesSentPerSecond * tsnForwardSize)
	// Assumme we will receive a tsn forward for every package received
	r.bytesReceivedPerSecond += uint64(r.packagesReceivedPerSecond * tsnForwardSize)

	// There is something called ‘delayed ACK’ (pion/sctp implements it) in order to
	// reduce the number acks. Ack timer interval is 200ms. Which means, there’s only
	// one ACK generated for all DATA chunks received during the time.
	r.bytesSentPerSecond += uint64(5 * (sctpHeaderSize + 8 + r.packagesReceivedPerSecond*4))
	r.bytesReceivedPerSecond += uint64(5 * (sctpHeaderSize + 8 + r.packagesSentPerSecond*4))

	return r
}

func scale(b uint64) (uint64, float64, float64, float64) {
	bps := b * 8
	kbps := float64(bps) / 1024.0
	mbps := kbps / 1024.0
	gbps := mbps / 1024.0

	return bps, kbps, mbps, gbps
}

func main() {
	appPayload := dataDescription{size: 9, amountPerSecond: 10, retries: 0}

	description := description{}
	description.in.connections = 150
	description.in.data = []dataDescription{appPayload}
	description.out.connections = 150 * 149
	description.out.data = []dataDescription{appPayload}

	r := upperBound(description)

	nPackages := r.packagesSentPerSecond
	bps, kbps, mbps, gbps := scale(r.bytesSentPerSecond)
	msg := "upper bound (out): %6d packages per second, %9d bytesps %8d bps %9.2f kbps %6.2f mbps %.2f gbps\n"
	fmt.Printf(msg, nPackages, r.bytesSentPerSecond, bps, kbps, mbps, gbps)

	nPackages = r.packagesReceivedPerSecond
	bps, kbps, mbps, gbps = scale(r.bytesReceivedPerSecond)
	msg = "upper bound  (in): %6d packages per second, %9d bytesps %8d bps %9.2f kbps %6.2f mbps %.2f gbps\n"
	fmt.Printf(msg, nPackages, r.bytesReceivedPerSecond, bps, kbps, mbps, gbps)

	fmt.Printf("total packages   : %6d packages per second\n", r.packagesSentPerSecond+r.packagesReceivedPerSecond)
}
