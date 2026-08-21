// Package interlocking implements the safety-critical conflict checks that
// gate route setup. A route may only be established when every one of its
// elements is free and compatible with already-active routes; any failure
// yields a ConflictItem so the caller can reject the route (and keep its
// signal at RED) rather than force an unsafe clearance.
package interlocking

import (
	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// Conflict kinds reported by Check.
const (
	CSectionOccupied   = "SECTION_OCCUPIED"
	CSectionLocked     = "SECTION_LOCKED"
	CPointConflict     = "POINT_CONFLICT"
	CFlankConflict     = "FLANK_CONFLICT"
	COpposingRoute     = "OPPOSING_ROUTE"
	CTerminalOccupied  = "TERMINAL_OCCUPIED"
)

// IsActive reports whether a route still holds interlocking resources
// (sections/points locked). Terminal, cancelled, conflict, failed and
// not-yet-processed (PENDING) routes hold nothing.
func IsActive(r *model.Route) bool {
	if r == nil {
		return false
	}
	switch r.State {
	case model.RoutePointsMoving,
		model.RouteLocked,
		model.RouteTrainComing,
		model.RouteTrainInRoute,
		model.RouteReleasing,
		model.RouteCancelPending:
		return true
	default:
		return false
	}
}

// Check evaluates whether `expanded` can be set up against the current yard
// state in `g` and the existing `routes`. It returns the list of conflict
// reasons (empty == clear to establish). newRouteID is the id the new route
// will take (empty when the route has not been persisted yet), used only to
// avoid self-conflict.
func Check(g *topology.Graph, routes []*model.Route, expanded *topology.ExpandedRoute, newRouteID string) []model.ConflictItem {
	if expanded == nil {
		return []model.ConflictItem{{Kind: CSectionOccupied, Detail: "nil expanded route"}}
	}
	var items []model.ConflictItem

	pathSet := make(map[string]bool, len(expanded.PathSections))
	for _, sid := range expanded.PathSections {
		pathSet[sid] = true
	}

	// 1. terminal occupied (压车 at the destination)
	if term, ok := g.Section(expanded.PathSections[len(expanded.PathSections)-1]); ok && term.Occupied() {
		items = append(items, model.ConflictItem{
			Kind:   CTerminalOccupied,
			RefID:  term.ID,
			Detail: "terminal section occupied",
		})
	}

	// 2. path sections: occupied or locked by an active route.
	for _, sid := range expanded.PathSections {
		s, ok := g.Section(sid)
		if !ok {
			items = append(items, model.ConflictItem{Kind: CSectionLocked, RefID: sid, Detail: "section not found"})
			continue
		}
		if s.Occupied() {
			items = append(items, model.ConflictItem{
				Kind:   CSectionOccupied,
				RefID:  s.ID,
				Detail: "path section occupied",
			})
			continue
		}
		if s.LockedByRoute != "" && s.LockedByRoute != newRouteID {
			if activeRouteByID(routes, s.LockedByRoute) {
				// Distinguish a head-on opposing route from a plain lock.
				if isOpposing(g, s.LockedByRoute, pathSet) {
					items = append(items, model.ConflictItem{
						Kind:   COpposingRoute,
						RefID:  s.LockedByRoute,
						Detail: "opposing active route guards a section on this path",
					})
				} else {
					items = append(items, model.ConflictItem{
						Kind:   CSectionLocked,
						RefID:  s.ID,
						Detail: "path section locked by active route " + s.LockedByRoute,
					})
				}
			}
		}
	}

	// 3. required points: locked by another active route, or faulted/timeout
	// (unless bypassed).
	for _, pr := range expanded.PointsRequired {
		p, ok := g.Point(pr.PointID)
		if !ok {
			items = append(items, model.ConflictItem{Kind: CPointConflict, RefID: pr.PointID, Detail: "point not found"})
			continue
		}
		if !p.Bypassed && (p.Status == model.PointFault || p.Status == model.PointOutOfCorrespondence) {
			items = append(items, model.ConflictItem{
				Kind:   CPointConflict,
				RefID:  p.ID,
				Detail: "point " + string(p.Status) + " (not bypassed)",
			})
			continue
		}
		if p.LockedByRoute != "" && p.LockedByRoute != newRouteID && activeRouteByID(routes, p.LockedByRoute) {
			items = append(items, model.ConflictItem{
				Kind:   CPointConflict,
				RefID:  p.ID,
				Detail: "point locked by active route " + p.LockedByRoute,
			})
			continue
		}
	}

	// 4. flank-protection points: same rules.
	for _, pr := range expanded.FlankProtection {
		p, ok := g.Point(pr.PointID)
		if !ok {
			items = append(items, model.ConflictItem{Kind: CFlankConflict, RefID: pr.PointID, Detail: "flank point not found"})
			continue
		}
		if !p.Bypassed && (p.Status == model.PointFault || p.Status == model.PointOutOfCorrespondence) {
			items = append(items, model.ConflictItem{
				Kind:   CFlankConflict,
				RefID:  p.ID,
				Detail: "flank point " + string(p.Status) + " (not bypassed)",
			})
			continue
		}
		if p.LockedByRoute != "" && p.LockedByRoute != newRouteID && activeRouteByID(routes, p.LockedByRoute) {
			items = append(items, model.ConflictItem{
				Kind:   CFlankConflict,
				RefID:  p.ID,
				Detail: "flank point locked by active route " + p.LockedByRoute,
			})
		}
	}

	return items
}

// activeRouteByID reports whether a route with the given id exists in routes
// and is currently active.
func activeRouteByID(routes []*model.Route, id string) bool {
	for _, r := range routes {
		if r.ID == id {
			return IsActive(r)
		}
	}
	return false
}

// isOpposing reports whether the active route `rid` has its origin signal
// guarding a section that lies on `pathSet` — i.e. a train from that route
// would enter this path head-on. We resolve the origin by scanning signals
// whose RouteID == rid: a signal cleared for `rid` whose guard section is on
// this path means a train is being released into our path from within.
func isOpposing(g *topology.Graph, rid string, pathSet map[string]bool) bool {
	for _, s := range g.Signals() {
		if s.RouteID == rid {
			if pathSet[s.GuardSectionID] {
				return true
			}
		}
	}
	return false
}
