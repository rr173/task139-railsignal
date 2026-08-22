// Package route implements the interlocked-route state machine: locking
// route resources (sections + points) on setup, advancing through train-
// detection states, and the section-by-section route release that follows a
// train out of the route. It also implements the rear-of-train protection
// that forbids cancelling (and unlocking) an occupied route until the train
// has cleared or a timed delay has elapsed.
package route

import (
	"task139-railsignal/internal/model"
)

// Machine drives a single route's lifecycle. It is a stateless set of pure
// transition helpers operating on a *model.Route and the yard graph
// (sections/points); the service layer persists the resulting state.
type Machine struct{}

// New returns the route machine.
func New() *Machine { return &Machine{} }

// LockResources marks the route's path sections and required/flank points as
// locked by this route. The caller has already passed interlocking.
func (m *Machine) LockResources(g ResourceGraph, r *model.Route) {
	routeID := r.ID
	for _, sid := range r.PathSections {
		g.SetSectionLock(sid, routeID)
	}
	for _, pr := range r.PointsRequired {
		g.SetPointLock(pr.PointID, routeID)
	}
	for _, pr := range r.FlankProtection {
		g.SetPointLock(pr.PointID, routeID)
	}
}

// UnlockAll releases every resource the route holds. Used when a route is
// cancelled while still unoccupied, or fully released.
func (m *Machine) UnlockAll(g ResourceGraph, r *model.Route) {
	for _, sid := range r.PathSections {
		g.SetSectionLock(sid, "")
	}
	for _, pr := range r.PointsRequired {
		g.SetPointLock(pr.PointID, "")
	}
	for _, pr := range r.FlankProtection {
		g.SetPointLock(pr.PointID, "")
	}
	r.ReleasedCount = len(r.PathSections)
}

// OnOccupancy advances a route based on a section-occupancy event. The state
// transitions are:
//   - LOCKED + approach occupied   -> TRAIN_COMING
//   - LOCKED/TRAIN_COMING + first path section occupied -> TRAIN_IN_ROUTE
//   - TRAIN_IN_ROUTE/RELEASING + a path section cleared -> release it; if the
//     cleared section is the last, RELEASED; otherwise RELEASING.
//
// `sectionID` is the newly occupied section; OnOccupancy only fires the
// approach/first-section transitions (mid-route occupancy does not change
// state — the train is simply within the route).
func (m *Machine) OnOccupancy(g ResourceGraph, r *model.Route, sectionID string, now int) (model.RouteState, bool) {
	if r.State == model.RouteConflict || r.State == model.RouteFailed || r.State.IsTerminal() {
		return r.State, false
	}
	if sectionID == r.ApproachSectionID && (r.State == model.RouteLocked) {
		r.State = model.RouteTrainComing
		return r.State, true
	}
	if sectionID == r.PathSections[0] && (r.State == model.RouteLocked || r.State == model.RouteTrainComing) {
		r.State = model.RouteTrainInRoute
		return r.State, true
	}
	return r.State, false
}

// OnClearance advances a route based on a section-clearance event. It releases
// the cleared path section (rear-of-train protection: only sections the train
// has actually left are released, and only in forward order). The last path
// section is only released once the train has fully left the route.
//
// Returns the new state and whether a section was released this call.
func (m *Machine) OnClearance(g ResourceGraph, r *model.Route, sectionID string, now int) (model.RouteState, bool) {
	if r.State == model.RouteConflict || r.State == model.RouteFailed || r.State.IsTerminal() {
		return r.State, false
	}
	if r.State != model.RouteTrainInRoute && r.State != model.RouteReleasing && r.State != model.RouteCancelPending {
		return r.State, false
	}
	// find position in path
	idx := -1
	for i, sid := range r.PathSections {
		if sid == sectionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return r.State, false
	}
	// can only release sections up to (but not including) the section currently
	// under the train. We release `idx` if all sections before it are already
	// released (forward order) AND idx is not the currently-occupied head.
	// Simplest correct rule: release `idx` only if every section 0..idx-1 is
	// already released.
	if idx > r.ReleasedCount {
		return r.State, false
	}
	if idx < r.ReleasedCount {
		// already released; ignore
		return r.State, false
	}
	// releasing idx: but the last section only once the train is gone.
	if idx == len(r.PathSections)-1 {
		// last section: require no path section still occupied
		for _, sid := range r.PathSections {
			if g.SectionOccupied(sid) {
				// train still somewhere in route; can't release the last
				return r.State, false
			}
		}
	}
	// release idx
	g.SetSectionLock(r.PathSections[idx], "")
	r.ReleasedCount = idx + 1
	if r.ReleasedCount >= len(r.PathSections) {
		// release all points too
		for _, pr := range r.PointsRequired {
			g.SetPointLock(pr.PointID, "")
		}
		for _, pr := range r.FlankProtection {
			g.SetPointLock(pr.PointID, "")
		}
		// if a cancel was pending, it is now satisfied -> cancelled; else released.
		if r.State == model.RouteCancelPending {
			r.State = model.RouteCancelled
		} else {
			r.State = model.RouteReleased
		}
		return r.State, true
	}
	if r.State == model.RouteTrainInRoute {
		r.State = model.RouteReleasing
	}
	return r.State, true
}

// Cancel requests cancellation of a route. If the route is LOCKED but no train
// has entered (first path section unoccupied), it cancels immediately and
// releases all resources. If a train has entered (first path section occupied),
// it goes to CANCEL_PENDING and the resources stay locked until the train
// clears (rear-of-train protection). Returns the new state.
func (m *Machine) Cancel(g ResourceGraph, r *model.Route, now int) (model.RouteState, error) {
	if r.State.IsTerminal() || r.State == model.RouteConflict || r.State == model.RouteFailed {
		return r.State, model.StateConflictf("route %s in state %s cannot be cancelled", r.ID, r.State)
	}
	// train has entered if the first path section is occupied.
	if g.SectionOccupied(r.PathSections[0]) {
		r.State = model.RouteCancelPending
		return r.State, nil
	}
	// no train: cancel immediately and release.
	r.State = model.RouteCancelled
	m.UnlockAll(g, r)
	return r.State, nil
}

// TickCancelDeadline advances a CANCEL_PENDING route: if the timed deadline has
// elapsed AND no path section is occupied, the route may now release to
// CANCELLED (delayed release after the train has gone, as a backstop for when
// occupancy detection is uncertain). Returns the new state.
func (m *Machine) TickCancelDeadline(g ResourceGraph, r *model.Route, now int) (model.RouteState, bool) {
	if r.State != model.RouteCancelPending {
		return r.State, false
	}
	if r.CancelDeadline > 0 && now < r.CancelDeadline {
		return r.State, false
	}
	// backstop release only when truly clear
	for _, sid := range r.PathSections {
		if g.SectionOccupied(sid) {
			return r.State, false
		}
	}
	r.State = model.RouteCancelled
	m.UnlockAll(g, r)
	return r.State, true
}

// Fail marks the route FAILED (e.g. a switch timed out during setup) and
// releases the resources it held so far — except a FAULTed point stays faulted.
// The owning signal is dropped to RED by the caller.
func (m *Machine) Fail(g ResourceGraph, r *model.Route) {
	r.State = model.RouteFailed
	m.UnlockAll(g, r)
}

// ResourceGraph is the yard surface the route machine mutates. It is a small
// interface so the machine stays decoupled from the concrete store/service.
type ResourceGraph interface {
	SetSectionLock(sectionID, routeID string)
	SetPointLock(pointID, routeID string)
	SectionOccupied(sectionID string) bool
}
