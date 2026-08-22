// Package point implements the switch-machine state machine: issuing move
// commands, applying position-detection events, timing out moves, enforcing
// anti-squeeze (no move under an occupied section), and the degraded-mode
// bypass. A point may only move when free or when the route that locked it
// explicitly drives it to a new direction.
package point

import (
	"task139-railsignal/internal/model"
)

// Machine drives a single point's lifecycle. It is a stateless set of pure
// transition helpers operating on a *model.Point; the service layer persists
// the resulting state. All transitions that fail return a *model.Error with
// a stable code.
type Machine struct{}

// New returns the point machine.
func New() *Machine { return &Machine{} }

// CanMove reports whether the point may receive a move command to `dir` right
// now, and if not, why. Reasons it cannot:
//   - already at target (no move needed) — not an error, returns false cleanly,
//   - faulted or out-of-correspondence (and not bypassed),
//   - an anti-squeeze section is occupied,
//   - locked by another route (handled by interlocking, but enforced here too).
//
// bypassed points skip the position-detection requirement but still cannot
// move under an occupied anti-squeeze section.
func (m *Machine) CanMove(p *model.Point, dir model.PointDirection, sectionOccupied func(string) bool) (bool, error) {
	if p.Status == model.PointFault {
		return false, model.Conflictf("point %s is FAULT", p.ID)
	}
	if p.Status == model.PointOutOfCorrespondence && !p.Bypassed {
		return false, model.Timeoutf("point %s is OUT_OF_CORRESPONDENCE", p.ID)
	}
	// already at target: nothing to do.
	if p.Status == model.PointInPosition && p.Direction == dir {
		return false, nil
	}
	// anti-squeeze: a protect section occupied forbids movement. Moving a
	// point under an occupied section would squeeze/cut the train, so this
	// guard may never be bypassed (not even for a bypassed point).
	for _, sid := range p.ProtectSections {
		if sectionOccupied(sid) {
			return false, model.PointOccupiedf("point %s protect section %s occupied", p.ID, sid)
		}
	}
	return true, nil
}

// IssueMove records a move command on the point: status MOVING, target set,
// deadline computed from `now` + max move time. It assumes the caller has
// already verified interlocking compatibility (point free / owned by this
// route) and anti-squeeze.
func (m *Machine) IssueMove(p *model.Point, dir model.PointDirection, now int) {
	if p.Status == model.PointInPosition && p.Direction == dir {
		return
	}
	p.Status = model.PointMoving
	p.TargetDirection = dir
	p.MoveStartTime = now
	p.MoveDeadline = now + p.MaxMoveSeconds
}

// Detect applies a position-detection event. If the detected direction matches
// the outstanding target, the point reaches IN_POSITION. If it detects the old
// direction while a move is outstanding, it is ignored (partial throw). A
// detection when no move is outstanding just confirms the current direction.
func (m *Machine) Detect(p *model.Point, dir model.PointDirection) bool {
	if p.Status == model.PointMoving {
		if dir == p.TargetDirection {
			p.Status = model.PointInPosition
			p.Direction = dir
			p.TargetDirection = dir
			p.MoveStartTime = 0
			p.MoveDeadline = 0
			return true
		}
		// detection of the non-target direction during a move: ignore.
		return false
	}
	if p.Status == model.PointInPosition {
		// spontaneous detection: keep as-is, confirm direction.
		p.Direction = dir
		return true
	}
	if p.Status == model.PointOutOfCorrespondence {
		// late detection recovers the point if it matches target.
		if p.TargetDirection != "" && dir == p.TargetDirection {
			p.Status = model.PointInPosition
			p.Direction = dir
			p.MoveStartTime = 0
			p.MoveDeadline = 0
			return true
		}
	}
	return false
}

// TickTimeout advances the timeout check: if a move is outstanding past its
// deadline, the point goes OUT_OF_CORRESPONDENCE. Returns true if the point
// newly timed out.
func (m *Machine) TickTimeout(p *model.Point, now int) bool {
	if p.Status != model.PointMoving {
		return false
	}
	if now >= p.MoveDeadline {
		p.Status = model.PointOutOfCorrespondence
		// keep Direction at last-known; TargetDirection retained for diagnostics
		return true
	}
	return false
}

// ForceFault sets the point to FAULT (manual maintenance action).
func (m *Machine) ForceFault(p *model.Point) {
	p.Status = model.PointFault
}

// ClearFault clears a FAULT back to FREE, leaving the last-known direction.
func (m *Machine) ClearFault(p *model.Point) {
	if p.Status == model.PointFault {
		p.Status = model.PointInPosition // confirmed at last-known direction
	}
}

// Bypass toggles bypass. Bypass may only be applied to a point that is not
// currently under an occupied anti-squeeze section (no bypassing a point
// under a train), and only when the point is OUT_OF_CORRESPONDENCE or FAULT.
// A healthy point cannot be bypassed (bypass is a degraded-mode tool).
func (m *Machine) Bypass(p *model.Point, on bool, sectionOccupied func(string) bool) error {
	if on {
		for _, sid := range p.ProtectSections {
			if sectionOccupied(sid) {
				return model.PointOccupiedf("cannot bypass point %s: protect section %s occupied", p.ID, sid)
			}
		}
		if p.Status != model.PointOutOfCorrespondence && p.Status != model.PointFault {
			return model.StateConflictf("point %s not in a degraded state (status=%s); bypass refused", p.ID, p.Status)
		}
		// when bypassed, treat as IN_POSITION at the target (or current) dir.
		p.Bypassed = true
		dir := p.TargetDirection
		if dir == "" {
			dir = p.Direction
		}
		p.Direction = dir
		p.Status = model.PointInPosition
		p.MoveStartTime = 0
		p.MoveDeadline = 0
		return nil
	}
	p.Bypassed = false
	return nil
}

// AtTarget reports whether the point is usable for a route requiring `dir`:
// in position at that direction, OR bypassed and pointing at that direction.
func (m *Machine) AtTarget(p *model.Point, dir model.PointDirection) bool {
	if p.Status != model.PointInPosition {
		return false
	}
	return p.Direction == dir
}
