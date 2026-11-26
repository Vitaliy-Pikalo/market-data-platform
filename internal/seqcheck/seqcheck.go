// Package seqcheck detects gaps in a per-connection message sequence number.
//
// The first number seen after New or Reset becomes the baseline. After that
// every message is expected to carry previous+1.
//
// Gaps are per connection, not per channel: Coinbase numbers every message on
// a connection with one sequence_num counter shared by all subscribed
// channels, and the counter starts over on a new connection. Use one Checker
// per connection and call Reset on reconnect; a gap across a reconnect is
// therefore never reported. A missing message's channel cannot be known, so
// callers attribute a gap to the channel of the message that exposed it (the
// first one received after the gap). Per-channel gap counts are thus "gaps
// noticed on this channel", not "messages lost from this channel".
package seqcheck

// Kind classifies one observation.
type Kind int

const (
	// OK: first message, or exactly the expected number.
	OK Kind = iota
	// Gap: the number jumped forward; Missing messages were skipped.
	Gap
	// Behind: the number is lower than or equal to one already seen
	// (duplicate or out-of-order delivery). The expectation is not moved back.
	Behind
)

func (k Kind) String() string {
	switch k {
	case OK:
		return "ok"
	case Gap:
		return "gap"
	case Behind:
		return "behind"
	}
	return "unknown"
}

// Result describes one observation. Expected and Got are set for Gap and Behind.
type Result struct {
	Kind     Kind
	Expected int64
	Got      int64
	Missing  int64 // Got-Expected for Gap, else 0
}

// Checker is not safe for concurrent use.
type Checker struct {
	started bool
	next    int64
}

// New returns a Checker with no baseline.
func New() *Checker { return &Checker{} }

// Reset forgets the baseline. Call it on every new connection.
func (c *Checker) Reset() { *c = Checker{} }

// Observe records seq and reports whether it was the expected number.
func (c *Checker) Observe(seq int64) Result {
	if !c.started {
		c.started, c.next = true, seq+1
		return Result{Kind: OK}
	}
	expected := c.next
	switch {
	case seq == expected:
		c.next = seq + 1
		return Result{Kind: OK}
	case seq > expected:
		c.next = seq + 1
		return Result{Kind: Gap, Expected: expected, Got: seq, Missing: seq - expected}
	default:
		return Result{Kind: Behind, Expected: expected, Got: seq}
	}
}
