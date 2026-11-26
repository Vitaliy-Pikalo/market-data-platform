// Package backoff implements capped exponential backoff with full jitter.
//
// The n-th consecutive retry (n starting at 0) waits a uniformly random
// duration in [0, min(Max, Base*2^n)). The attempt counter resets once a
// connection has stayed healthy for at least ResetAfter.
package backoff

import (
	"math/rand/v2"
	"time"
)

// Defaults used by the recorder.
const (
	DefaultBase       = 500 * time.Millisecond
	DefaultMax        = 30 * time.Second
	DefaultResetAfter = 60 * time.Second
)

// Backoff is not safe for concurrent use.
type Backoff struct {
	Base       time.Duration
	Max        time.Duration
	ResetAfter time.Duration

	// Rand returns a value in [0, n). Defaults to math/rand/v2.Int64N.
	// Overridable for tests.
	Rand func(n int64) int64

	attempt int
}

// New returns a Backoff with the given parameters.
func New(base, max, resetAfter time.Duration) *Backoff {
	return &Backoff{Base: base, Max: max, ResetAfter: resetAfter}
}

// Next returns how long to wait before the next connection attempt.
// healthy is how long the previous connection stayed up (0 if it never
// connected); if it reaches ResetAfter the exponent starts over.
func (b *Backoff) Next(healthy time.Duration) time.Duration {
	if b.ResetAfter > 0 && healthy >= b.ResetAfter {
		b.attempt = 0
	}
	c := b.Cap(b.attempt)
	b.attempt++
	if c <= 0 {
		return 0
	}
	rnd := b.Rand
	if rnd == nil {
		rnd = rand.Int64N
	}
	return time.Duration(rnd(int64(c)))
}

// Attempt returns the number of consecutive retries since the last reset.
func (b *Backoff) Attempt() int { return b.attempt }

// Reset sets the attempt counter back to zero.
func (b *Backoff) Reset() { b.attempt = 0 }

// Cap returns the upper bound (exclusive) of the wait for the given attempt:
// min(Max, Base*2^attempt), without overflowing.
func (b *Backoff) Cap(attempt int) time.Duration {
	c := b.Base
	for i := 0; i < attempt; i++ {
		if c >= b.Max || c > b.Max/2 {
			return b.Max
		}
		c *= 2
	}
	if c > b.Max {
		return b.Max
	}
	return c
}
