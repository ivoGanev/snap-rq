package requests

import (
	"math"
	"time"
)

// usage.go — how much a request is used lately, as one fading score: each
// send adds 1, and the score halves every usageHalfLife. Only the score and
// the last send time are stored (use_score, last_used_at), so there is no
// usage log to grow. The levels the app shows (high / medium / low) are
// decided here and nowhere else.

const usageHalfLife = 7 * 24 * time.Hour

// Levels by the score faded to now. Roughly: high is several sends in the
// last week or so, medium a couple of recent ones, low anything in the last
// few weeks. Below usageLow a request shows no usage.
const (
	usageHigh   = 5.0
	usageMedium = 2.0
	usageLow    = 0.1
)

// fadedScore is a stored score faded from its last send to now.
func fadedScore(score float64, lastUsedNano int64, now time.Time) float64 {
	if score <= 0 || lastUsedNano <= 0 {
		return 0
	}
	elapsed := now.Sub(time.Unix(0, lastUsedNano))
	if elapsed <= 0 {
		return score
	}
	return score * math.Pow(0.5, float64(elapsed)/float64(usageHalfLife))
}

// usageLevel is what the app shows for a faded score ("" for none).
func usageLevel(score float64) string {
	switch {
	case score >= usageHigh:
		return "high"
	case score >= usageMedium:
		return "medium"
	case score >= usageLow:
		return "low"
	}
	return ""
}
