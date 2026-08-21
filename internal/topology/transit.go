package topology

import (
	"fmt"

	"task139-railsignal/internal/fixed"
	"task139-railsignal/internal/model"
)

// TransitTime computes the expected train transit time (integer seconds) for a
// train travelling at a given speed to traverse a sequence of sections. It is
// the sum over each section of ceil(length_m / speed_dm_s), where speed_dm_s
// is the deci-meters-per-second step count (0.1 m/s precision) converted from
// a kilometres-per-hour input.
//
// This drives the rear-of-train release timing used by the cancel backstop
// (a cancelled route that is occupied may only release after the train has
// had time to clear the whole path, i.e. transit + a safety margin).
func (g *Graph) TransitTime(sectionIDs []string, speedKmh int) (int, error) {
	if speedKmh <= 0 {
		return 0, fmt.Errorf("speed must be > 0")
	}
	speedDms := fixed.KmhToDms(speedKmh)
	if speedDms <= 0 {
		speedDms = 1
	}
	total := 0
	for _, sid := range sectionIDs {
		s, ok := g.sections[sid]
		if !ok {
			return 0, fmt.Errorf("section %s not found", sid)
		}
		// seconds to cross one section = ceil(length_m / speed_dm_s)
		total += fixed.Ceil(s.LengthM, speedDms)
	}
	return total, nil
}

// SectionTransitTime is the single-section variant.
func (g *Graph) SectionTransitTime(sectionID string, speedKmh int) (int, error) {
	s, ok := g.sections[sectionID]
	if !ok {
		return 0, fmt.Errorf("section %s not found", sectionID)
	}
	if speedKmh <= 0 {
		return 0, fmt.Errorf("speed must be > 0")
	}
	speedDms := fixed.KmhToDms(speedKmh)
	if speedDms <= 0 {
		speedDms = 1
	}
	return fixed.Ceil(s.LengthM, speedDms), nil
}

// maxTransitMargin is the safety margin added to the transit time when
// computing a cancel-pending backstop deadline.
const maxTransitMargin = 30

// CancelBackstopDeadline returns the sim-clock deadline at which a cancelled-
// pending route may release as a backstop: openedAt + transit + margin.
func (g *Graph) CancelBackstopDeadline(openedAt int, sectionIDs []string, speedKmh int) int {
	t, err := g.TransitTime(sectionIDs, speedKmh)
	if err != nil || t <= 0 {
		// fall back to a fixed margin if the path is empty/uncomputable
		return openedAt + fixed.Max(120, maxTransitMargin)
	}
	return openedAt + t + maxTransitMargin
}

var _ = model.KindTrack // keep model import referenced
