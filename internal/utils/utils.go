package utils

import (
	"time"
)

func NowMs() float64 {
	return Milliseconds(time.Now())
}

func Milliseconds(t time.Time) float64 {
	return float64(t.UnixNano() / int64(time.Millisecond))
}

func Max(a int, b int) int {
	if a > b {
		return a
	}

	return b
}

func Min(a int, b int) int {
	if a < b {
		return a
	}

	return b
}
