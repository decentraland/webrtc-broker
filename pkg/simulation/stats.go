package simulation

import (
	"time"
)

// AvgFreq is used to track the avg frequency between messages
type AvgFreq struct {
	Total   time.Duration
	Samples uint16
	Last    time.Time
}

// Seen is called when a new message is received
func (a *AvgFreq) Seen(t time.Time) {
	if a.Last.IsZero() {
		a.Last = t
	} else {
		duration := t.Sub(a.Last)
		a.Last = t
		a.Total += duration
		a.Samples++
	}
}

// Avg return the avg frequency between message
func (a *AvgFreq) Avg() float64 {
	totalMs := float64(a.Total.Nanoseconds() / int64(time.Millisecond))
	return totalMs / float64(a.Samples)
}

// Stats is the main stats structure
type Stats struct {
	avgFreq  AvgFreq
	LastSeen time.Time
}

// Seen is called when a new message is received
func (s *Stats) Seen(t time.Time) {
	s.avgFreq.Seen(t)
	s.LastSeen = t
}

// Avg return the avg frequency between message
func (s *Stats) Avg() float64 {
	return s.avgFreq.Avg()
}

// Samples return the amount of collected samples so far
func (s *Stats) Samples() uint16 {
	return s.avgFreq.Samples
}
