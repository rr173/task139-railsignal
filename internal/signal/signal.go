// Package signal computes whether a signal may be cleared and to which aspect,
// following the railway fail-safe principle: a signal is clear (showing a
// proceed aspect) ONLY while every opening condition holds, and it returns to
// RED the instant any condition fails. The opening conditions are:
//   1. the owning route's switches are all IN_POSITION at the required direction,
//   2. flank protection is set and detected,
//   3. every route section is free (not occupied, not locked by another route),
//   4. no opposing route is active.
// Straight routes show GREEN; diverging routes show DOUBLE_YELLOW.
package signal

import (
	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// Controller is a stateless helper that decides aspect and enforces fail-safe.
type Controller struct{}

// New returns a controller.
func New() *Controller { return &Controller{} }

// CanClear reports whether the signal guarding `route` may be cleared to a
// proceed aspect given the current yard state. It returns the aspect to show
// (GREEN for straight, DOUBLE_YELLOW for diverging) and true, or RED and
// false when any condition fails.
func (c *Controller) CanClear(g *topology.Graph, r *model.Route, routeID string) (model.SignalAspect, bool) {
	// 1. every required point IN_POSITION at required direction (bypassed ok).
	for _, pr := range r.PointsRequired {
		p, ok := g.Point(pr.PointID)
		if !ok {
			return model.AspectRed, false
		}
		if p.Bypassed {
			// bypassed point must be set at the required direction
			if p.Direction != pr.Direction {
				return model.AspectRed, false
			}
			continue
		}
		if p.Status != model.PointInPosition || p.Direction != pr.Direction {
			return model.AspectRed, false
		}
	}
	// 2. flank protection set and detected.
	for _, pr := range r.FlankProtection {
		p, ok := g.Point(pr.PointID)
		if !ok {
			return model.AspectRed, false
		}
		if p.Bypassed {
			if p.Direction != pr.Direction {
				return model.AspectRed, false
			}
			continue
		}
		if p.Status != model.PointInPosition || p.Direction != pr.Direction {
			return model.AspectRed, false
		}
	}
	// 3. every route section free (not occupied) and not locked by another route.
	for _, sid := range r.PathSections {
		s, ok := g.Section(sid)
		if !ok {
			return model.AspectRed, false
		}
		if s.Occupied() {
			return model.AspectRed, false
		}
		if s.LockedByRoute != "" && s.LockedByRoute != routeID {
			return model.AspectRed, false
		}
	}
	// 4. no opposing route: a signal cleared for another route whose guard
	// section lies on this route means a head-on movement — refuse.
	for _, sig := range g.Signals() {
		if sig.ID == r.OriginSignalID {
			continue
		}
		if sig.RouteID != "" && sig.RouteID != routeID {
			// another route's cleared signal guarding a section on our path?
			if r.PathSections[0] == sig.GuardSectionID {
				return model.AspectRed, false
			}
		}
	}
	if r.DivergingRoute() {
		return model.AspectGreen, true
	}
	return model.AspectGreen, true
}

// AspectFor computes the proceed aspect for a route (straight=GREEN, diverging
// over >=2 points=DOUBLE_YELLOW, single-point diverging=YELLOW).
func (c *Controller) AspectFor(r *model.Route) model.SignalAspect {
	if !r.DivergingRoute() {
		return model.AspectGreen
	}
	if len(r.PointsRequired) > 1 {
		return model.AspectDoubleYellow
	}
	return model.AspectYellow
}

// Enforce re-evaluates the signal for an active route and clamps it back to
// RED whenever CanClear no longer holds. It returns the aspect that should be
// displayed. The caller (service) persists the change.
func (c *Controller) Enforce(g *topology.Graph, r *model.Route, routeID string) model.SignalAspect {
	asp, ok := c.CanClear(g, r, routeID)
	if !ok {
		return model.AspectRed
	}
	return asp
}
