// Package clock provides an injectable monotonic clock abstraction. The
// engine is driven by a discrete integer second clock (sim time) so that
// switch timeouts, timed route release and train-detection sequencing are
// deterministic and independent of wall time.
package clock

// Clock reports the current simulation time in integer seconds since the
// yard base epoch.
type Clock interface {
	// Now returns the current sim seconds.
	Now() int
}

// SimClock is an in-memory, manually-advancing clock used by the service
// layer and tests. Advance must be called to move time forward; it never
// auto-ticks from wall time.
type SimClock struct {
	t int
}

// NewSim constructs a sim clock starting at base 0.
func NewSim() *SimClock { return &SimClock{} }

// Now returns the current sim time.
func (c *SimClock) Now() int { return c.t }

// Advance moves the clock forward by delta seconds (delta must be >= 0) and
// returns the new time.
func (c *SimClock) Advance(delta int) int {
	if delta < 0 {
		delta = 0
	}
	c.t += delta
	return c.t
}

// Set repositions the clock (only used by recovery to align with persisted
// clock); newT must be >= current time.
func (c *SimClock) Set(newT int) int {
	if newT < c.t {
		return c.t
	}
	c.t = newT
	return c.t
}

// RealClock is a wall-time clock for the live server. It is not used by the
// deterministic smoke test; the live service uses a sim clock advanced by the
// /clock/advance API.
type RealClock struct{}

// Now returns 0 for the live server; the live engine also drives a sim clock.
func (RealClock) Now() int { return 0 }
