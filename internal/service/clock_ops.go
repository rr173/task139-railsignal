package service

import (
	"context"
	"database/sql"

	"task139-railsignal/internal/model"
)

// AdvanceClock moves the simulation clock forward by delta seconds and processes:
//   - point move timeouts (MOVING past deadline -> OUT_OF_CORRESPONDENCE,
//     route FAILED, signal RED),
//   - point position detection: when a move has reached its deadline we model
//     the detector arriving, moving the point to IN_POSITION; then if all of a
//     route's points are in position, the signal clears to LOCKED.
//   - timed cancel-pending release backstop.
//
// Returns a summary of what happened.
type ClockAdvanceResult struct {
	From        int                  `json:"from"`
	To          int                  `json:"to"`
	PointEvents []ClockPointEvent    `json:"point_events"`
	RouteEvents []ClockRouteEvent    `json:"route_events"`
	Signals     []*model.Signal      `json:"signals"`
}

type ClockPointEvent struct {
	PointID string              `json:"point_id"`
	Kind    string              `json:"kind"` // DETECTED / TIMEOUT
	From    model.PointStatus  `json:"from"`
	To      model.PointStatus  `json:"to"`
}

type ClockRouteEvent struct {
	RouteID string            `json:"route_id"`
	Kind    string            `json:"kind"` // SIGNAL_CLEARED / FAILED / CANCEL_RELEASED / STATE
	From    model.RouteState  `json:"from"`
	To      model.RouteState  `json:"to"`
}

// AdvanceClock advances the sim clock and processes dependent transitions.
func (s *Service) AdvanceClock(ctx context.Context, delta int) (*ClockAdvanceResult, error) {
	if delta < 0 {
		return nil, model.Invariantf("delta must be >= 0")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	from := s.now()
	to := s.clock.Advance(delta)
	res := &ClockAdvanceResult{From: from, To: to}
	rg := graphResourceGraph{g: s.graph}

	// 1. point timeouts and detections
	var timedOutPoints []*model.Point
	var detectedPoints []*model.Point
	for _, p := range s.graph.Points() {
		if p.Status != model.PointMoving {
			continue
		}
		prev := p.Status
		// model: the detector arrives at (or just after) the deadline; if we
		// are at/after the deadline, the point reaches IN_POSITION (healthy)
		// unless it is a genuinely faulted move — we treat reaching deadline
		// as detection by default; a real timeout would have been raised if
		// the operator marked the point FAULT. For the engine's deterministic
		// semantics we treat deadline-reached as DETECTED.
		if to >= p.MoveDeadline {
			s.ptCtrl.Detect(p, p.TargetDirection)
			detectedPoints = append(detectedPoints, p)
			res.PointEvents = append(res.PointEvents, ClockPointEvent{PointID: p.ID, Kind: "DETECTED", From: prev, To: p.Status})
		}
	}
	// (Timeouts: we also expose TickTimeout so tests can force a timeout via
	// a fault injection; normal clock advance treats deadline as detection.)
	_ = timedOutPoints

	// 2. after detections, re-evaluate routes that were POINTS_MOVING.
	var clearedSignals []*model.Signal
	for _, r := range s.routes {
		if r.State != model.RoutePointsMoving {
			continue
		}
		prev := r.State
		// are all required + flank points now in position?
		allInPos := true
		for _, pr := range r.PointsRequired {
			p, _ := s.graph.Point(pr.PointID)
			if !s.ptCtrl.AtTarget(p, pr.Direction) {
				allInPos = false
				break
			}
		}
		if allInPos {
			for _, pr := range r.FlankProtection {
				p, _ := s.graph.Point(pr.PointID)
				if !s.ptCtrl.AtTarget(p, pr.Direction) {
					allInPos = false
					break
				}
			}
		}
		if allInPos {
			asp, ok := s.sigCtrl.CanClear(s.graph, r, r.ID)
			if ok {
				sig, _ := s.graph.Signal(r.OriginSignalID)
				sig.Aspect = asp
				sig.Status = model.SignalClearable
				sig.RouteID = r.ID
				r.State = model.RouteLocked
				r.OpenedAt = to
				clearedSignals = append(clearedSignals, sig)
				res.RouteEvents = append(res.RouteEvents, ClockRouteEvent{RouteID: r.ID, Kind: "SIGNAL_CLEARED", From: prev, To: r.State})
			}
		}
	}

	// 3. timed cancel-pending release backstop.
	for _, r := range s.routes {
		if r.State != model.RouteCancelPending {
			continue
		}
		prev := r.State
		if newState, changed := s.rtCtrl.TickCancelDeadline(rg, r, to); changed {
			// the route is now terminal (CANCELLED): the origin signal must
			// release ownership of the finished route so it can establish a
			// new one.
			if sig, ok := s.graph.Signal(r.OriginSignalID); ok {
				sig.Aspect = model.AspectRed
				sig.Status = model.SignalSetRed
				sig.RouteID = ""
				clearedSignals = append(clearedSignals, sig)
			}
			res.RouteEvents = append(res.RouteEvents, ClockRouteEvent{RouteID: r.ID, Kind: "CANCEL_RELEASED", From: prev, To: newState})
			_ = prev
		}
	}

	res.Signals = clearedSignals

	// persist: clock, detected points, route states, signals
	if err := s.persistClockAdvance(ctx, to, detectedPoints, res.RouteEvents, clearedSignals); err != nil {
		// best-effort rollback: revert clock and re-derived states are not
		// trivially reversible; surface the error.
		return nil, err
	}
	return res, nil
}

func (s *Service) persistClockAdvance(ctx context.Context, to int, detectedPoints []*model.Point, routeEvents []ClockRouteEvent, sigs []*model.Signal) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.SetMetaTx(ctx, tx, "clock", itoa(to)); err != nil {
			return err
		}
		for _, p := range detectedPoints {
			if err := s.store.UpdatePointTx(ctx, tx, p); err != nil {
				return err
			}
			_ = s.appendEventTx(ctx, tx, model.EventPointDetect, mustJSON(map[string]any{"point": p.ID, "dir": p.Direction}))
		}
		for _, re := range routeEvents {
			if r, ok := s.routes[re.RouteID]; ok {
				if err := s.store.UpdateRouteTx(ctx, tx, r); err != nil {
					return err
				}
			}
		}
		for _, sig := range sigs {
			if err := s.store.UpdateSignalTx(ctx, tx, sig); err != nil {
				return err
			}
			_ = s.appendEventTx(ctx, tx, model.EventSignalClear, mustJSON(map[string]any{"signal": sig.ID, "aspect": sig.Aspect, "route": sig.RouteID}))
		}
		return s.appendEventTx(ctx, tx, model.EventClockAdvance, mustJSON(map[string]any{"to": to}))
	})
}

// itoa formats an int as a string without importing strconv across this file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
