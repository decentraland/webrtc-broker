package simulation

import (
	"time"
)

type AvgFreq struct {
	Total   time.Duration
	Samples uint16
	Last    time.Time
}

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

func (a *AvgFreq) Avg() float64 {
	totalMs := float64(a.Total.Nanoseconds() / int64(time.Millisecond))
	return totalMs / float64(a.Samples)
}

type Stats struct {
	avgFreq  AvgFreq
	LastSeen time.Time
}

func (s *Stats) Seen(t time.Time) {
	s.avgFreq.Seen(t)
	s.LastSeen = t
}

func (s *Stats) Avg() float64 {
	return s.avgFreq.Avg()
}
