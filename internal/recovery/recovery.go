// Package recovery rebuilds the in-memory yard state from the authoritative
// SQLite tables after a restart. LoadAll reads every entity into a fresh
// topology.Graph; ReconcileAll recomputes derived interlocking state
// (point locks, section locks, route progress, signal open conditions) so the
// resumed state matches what was running before the crash.
package recovery

import (
	"context"
	"fmt"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/store"
	"task139-railsignal/internal/topology"
)

// LoadSnapshot is the full picture of the yard after a restart: the rebuilt
// graph plus all routes (active or not) loaded from the routes table.
type LoadSnapshot struct {
	Graph  *topology.Graph
	Routes []*model.Route
}

// LoadAll reads nodes, sections, points, signals and routes from the store
// and constructs a topology.Graph that mirrors the persisted layout. It also
// restores the persisted clock.
func LoadAll(ctx context.Context, st *store.Store) (*LoadSnapshot, error) {
	g := topology.New()

	nodes, err := st.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	for _, n := range nodes {
		if err := g.AddNode(n); err != nil {
			return nil, fmt.Errorf("rebuild node %s: %w", n.ID, err)
		}
	}
	sections, err := st.ListSections(ctx)
	if err != nil {
		return nil, fmt.Errorf("load sections: %w", err)
	}
	for _, s := range sections {
		if err := g.AddSection(s); err != nil {
			return nil, fmt.Errorf("rebuild section %s: %w", s.ID, err)
		}
	}
	// restore runtime occupancy/lock fields (AddSection copies only layout
	// fields and zeroes runtime, so we must overlay them here).
	for _, s := range sections {
		if got, ok := g.Section(s.ID); ok {
			got.OccupancyCnt = s.OccupancyCnt
			got.LockedByRoute = s.LockedByRoute
		}
	}
	points, err := st.ListPoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("load points: %w", err)
	}
	for _, p := range points {
		if err := g.AddPoint(p); err != nil {
			return nil, fmt.Errorf("rebuild point %s: %w", p.ID, err)
		}
	}
	signals, err := st.ListSignals(ctx)
	if err != nil {
		return nil, fmt.Errorf("load signals: %w", err)
	}
	for _, s := range signals {
		if err := g.AddSignal(s); err != nil {
			return nil, fmt.Errorf("rebuild signal %s: %w", s.ID, err)
		}
	}
	routes, err := st.ListRoutes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("load routes: %w", err)
	}
	return &LoadSnapshot{Graph: g, Routes: routes}, nil
}

// ReconcileAll recomputes derived state over a loaded snapshot and returns the
// reconciled graph + routes. It performs NO persistence; the caller may persist
// any drift afterwards. The computation is deterministic and idempotent.
//
// Steps:
//  1. Section locks: for each non-terminal active route, lock its unreleased
//     path sections (those at index >= ReleasedCount) to that route.
//  2. Point locks: for each non-terminal active route, lock its required and
//     flank-protection points to that route.
//  3. Route progress & state: re-derive each route's state from the current
//     section occupancy and its released_count:
//       - any path section occupied with index >= ReleasedCount and it's the
//         first un-released section -> TRAIN_IN_ROUTE
//       - approach section occupied and state LOCKED-ish -> TRAIN_COMING
//       - all path sections free and released_count == len(path) -> RELEASED
//       - else keep persisted state if it is still consistent
//  4. Signal open condition: re-evaluate each active route's origin signal; if
//     the route no longer satisfies the open conditions, drop signal to RED.
func ReconcileAll(snap *LoadSnapshot) (*topology.Graph, []*model.Route) {
	g := snap.Graph
	routes := snap.Routes

	// 1 & 2: rebuild locks from active routes.
	for _, r := range routes {
		if !r.State.IsActive() {
			continue
		}
		for i, sid := range r.PathSections {
			if i < r.ReleasedCount {
				continue // already released
			}
			if s, ok := g.Section(sid); ok {
				s.LockedByRoute = r.ID
			}
		}
		for _, pr := range r.PointsRequired {
			if p, ok := g.Point(pr.PointID); ok {
				p.LockedByRoute = r.ID
			}
		}
		for _, pr := range r.FlankProtection {
			if p, ok := g.Point(pr.PointID); ok {
				p.LockedByRoute = r.ID
			}
		}
	}

	// 3: re-derive route progress/state from occupancy.
	for _, r := range routes {
		if !r.State.IsActive() {
			continue
		}
		reconcileRouteState(g, r)
	}

	// 4: re-evaluate signals for active routes.
	for _, r := range routes {
		if !r.State.IsActive() {
			continue
		}
		if sig, ok := g.Signal(r.OriginSignalID); ok {
			if !routeStillOpenable(g, r) {
				sig.Aspect = model.AspectRed
				sig.Status = model.SignalSetRed
				// the route may no longer be LOCKED if conditions collapsed;
				// but we do not auto-fail here — operator action handles that.
			} else {
				sig.Aspect = aspectForRoute(r)
				sig.Status = model.SignalClearable
				sig.RouteID = r.ID
			}
		}
	}
	return g, routes
}

// reconcileRouteState re-derives a route's state from occupancy and released
// count, only relaxing/advancing forward; it never revives a terminal route.
func reconcileRouteState(g *topology.Graph, r *model.Route) {
	// count occupied path sections at index >= releasedCount (un-released tail)
	unreleasedOccupied := false
	for i, sid := range r.PathSections {
		if i < r.ReleasedCount {
			continue
		}
		if s, ok := g.Section(sid); ok && s.Occupied() {
			unreleasedOccupied = true
			break
		}
	}
	// all released?
	if r.ReleasedCount > len(r.PathSections) {
		r.State = model.RouteReleased
		return
	}
	// train is in the unreleased tail
	if unreleasedOccupied {
		// first unreleased section occupied => train in route
		if s, ok := g.Section(r.PathSections[r.ReleasedCount]); ok && s.Occupied() {
			if r.State == model.RouteLocked || r.State == model.RouteTrainComing || r.State == model.RouteReleasing {
				r.State = model.RouteTrainInRoute
			}
			return
		}
		r.State = model.RouteTrainInRoute
		return
	}
	// approach occupied but no train in route yet => coming
	if r.ApproachSectionID != "" {
		if s, ok := g.Section(r.ApproachSectionID); ok && s.Occupied() {
			if r.State == model.RouteLocked {
				r.State = model.RouteTrainComing
			}
			return
		}
	}
	// nothing occupied in unreleased tail, not fully released => still locked,
	// or releasing (train has left some but not all). Keep persisted state if
	// it is consistent with "no train present"; otherwise fall back to LOCKED.
	switch r.State {
	case model.RouteLocked, model.RoutePointsMoving, model.RouteReleasing, model.RouteTrainComing, model.RouteCancelPending, model.RouteTrainInRoute:
		// no train currently in the unreleased tail: clamp to LOCKED unless
		// we are mid-release (released_count>0).
		if r.ReleasedCount > 0 {
			r.State = model.RouteReleasing
		} else {
			r.State = model.RouteLocked
		}
	}
}

// routeStillOpenable checks whether the route's origin signal may still show a
// proceed aspect: required & flank points in position (or bypassed at the
// right direction), path sections free and self-locked, no opposing clearance.
func routeStillOpenable(g *topology.Graph, r *model.Route) bool {
	for _, pr := range r.PointsRequired {
		p, ok := g.Point(pr.PointID)
		if !ok {
			return false
		}
		if p.Bypassed {
			if p.Direction != pr.Direction {
				return false
			}
			continue
		}
		if p.Status != model.PointInPosition || p.Direction != pr.Direction {
			return false
		}
	}
	for _, pr := range r.FlankProtection {
		p, ok := g.Point(pr.PointID)
		if !ok {
			return false
		}
		if p.Bypassed {
			if p.Direction != pr.Direction {
				return false
			}
			continue
		}
		if p.Status != model.PointInPosition || p.Direction != pr.Direction {
			return false
		}
	}
	for _, sid := range r.PathSections {
		s, ok := g.Section(sid)
		if !ok {
			return false
		}
		if s.Occupied() {
			return false
		}
		if s.LockedByRoute != "" && s.LockedByRoute != r.ID {
			return false
		}
	}
	return true
}

// aspectFor returns the proceed aspect for a route.
func aspectForRoute(r *model.Route) model.SignalAspect {
	if !r.DivergingRoute() {
		return model.AspectGreen
	}
	if len(r.PointsRequired) > 1 {
		return model.AspectDoubleYellow
	}
	return model.AspectYellow
}
