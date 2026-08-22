package service

import (
	"context"
	"database/sql"
	"strings"

	"task139-railsignal/internal/idlib"
	"task139-railsignal/internal/interlocking"
	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// RouteRequest sets up a route from an origin signal to a terminal section.
type RouteRequest struct {
	OriginSignalID    string `json:"origin_signal_id"`
	TerminalSectionID string `json:"terminal_section_id"`
}

// RouteResult is the POST /routes response.
type RouteResult struct {
	Route    *model.Route            `json:"route"`
	Expanded *topology.ExpandedRoute `json:"expanded"`
	Cleared  bool                    `json:"cleared"`
	Aspect   model.SignalAspect      `json:"aspect"`
}

// RequestRoute expands, interlocks, locks resources, moves points and (if all
// points are already in position) clears the signal. If any interlocking check
// fails it persists a CONFLICT route record and returns the conflict detail.
func (s *Service) RequestRoute(ctx context.Context, req RouteRequest) (*RouteResult, error) {
	if strings.TrimSpace(req.OriginSignalID) == "" || strings.TrimSpace(req.TerminalSectionID) == "" {
		return nil, model.Invariantf("origin_signal_id and terminal_section_id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sig, ok := s.graph.Signal(req.OriginSignalID)
	if !ok {
		return nil, model.NotFoundf("signal %s not found", req.OriginSignalID)
	}
	term, ok := s.graph.Section(req.TerminalSectionID)
	if !ok {
		return nil, model.NotFoundf("terminal section %s not found", req.TerminalSectionID)
	}
	// a signal already cleared for a route cannot originate another.
	if sig.RouteID != "" {
		return nil, model.StateConflictf("signal %s already cleared for route %s", sig.ID, sig.RouteID)
	}

	expanded, err := s.graph.Expand(sig.ID, term.ID)
	if err != nil {
		return nil, err
	}

	// interlocking check
	conflicts := interlocking.Check(s.graph, s.activeRoutes(), expanded, "")
	if len(conflicts) > 0 {
		// persist a CONFLICT record for audit and return it.
		r := &model.Route{
			ID:                idlib.NewRouteID(),
			Code:              "RT-" + sig.Code + "->" + term.Code,
			OriginSignalID:    sig.ID,
			TerminalSectionID: term.ID,
			TerminalKind:      term.Kind,
			State:             model.RouteConflict,
			PathSections:      expanded.PathSections,
			PointsRequired:    expanded.PointsRequired,
			FlankProtection:   expanded.FlankProtection,
			ApproachSectionID: expanded.ApproachSectionID,
			ConflictDetail:    conflicts,
		}
		_ = s.persistConflictRoute(ctx, r)
		return &RouteResult{Route: r, Expanded: expanded, Cleared: false, Aspect: model.AspectRed}, nil
	}

	// build the route record
	r := &model.Route{
		ID:                idlib.NewRouteID(),
		Code:              "RT-" + sig.Code + "->" + term.Code,
		OriginSignalID:    sig.ID,
		TerminalSectionID: term.ID,
		TerminalKind:      term.Kind,
		State:             model.RoutePending,
		PathSections:      expanded.PathSections,
		PointsRequired:    expanded.PointsRequired,
		FlankProtection:   expanded.FlankProtection,
		ApproachSectionID: expanded.ApproachSectionID,
	}
	now := s.now()
	rg := graphResourceGraph{g: s.graph}

	// Lock route resources (sections + points).
	s.rtCtrl.LockResources(rg, r)

	// Compute the expected train transit time across the path (used for the
	// cancel-pending backstop deadline so a cancelled occupied route only
	// releases once a train could have cleared the path).
	r.TransitSec, _ = s.graph.TransitTime(r.PathSections, defaultTrainSpeedKmh)

	// Issue point moves for any required/flank point not already in position.
	anyMoving := false
	for _, pr := range r.PointsRequired {
		p, _ := s.graph.Point(pr.PointID)
		if ok, err := s.ptCtrl.CanMove(p, pr.Direction, s.sectionOccupiedFn()); err != nil {
			// anti-squeeze or fault: fail the route, release resources.
			s.rtCtrl.Fail(rg, r)
			_ = s.persistRouteTx(ctx, r)
			return nil, err
		} else if !ok {
			continue // already at target
		}
		s.ptCtrl.IssueMove(p, pr.Direction, now)
		anyMoving = true
	}
	for _, pr := range r.FlankProtection {
		p, _ := s.graph.Point(pr.PointID)
		if ok, err := s.ptCtrl.CanMove(p, pr.Direction, s.sectionOccupiedFn()); err != nil {
			s.rtCtrl.Fail(rg, r)
			_ = s.persistRouteTx(ctx, r)
			return nil, err
		} else if !ok {
			continue
		}
		s.ptCtrl.IssueMove(p, pr.Direction, now)
		anyMoving = true
	}

	if anyMoving {
		r.State = model.RoutePointsMoving
		// signal stays RED until points detected.
	} else {
		// all points in position: clear the signal.
		asp, ok := s.sigCtrl.CanClear(s.graph, r, r.ID)
		if !ok {
			// should not happen after a clean interlocking check, but be safe.
			r.State = model.RouteFailed
			s.rtCtrl.UnlockAll(rg, r)
			_ = s.persistRouteTx(ctx, r)
			return nil, model.StateConflictf("route %s could not be cleared after setup", r.ID)
		}
		sig.Aspect = asp
		sig.Status = model.SignalClearable
		sig.RouteID = r.ID
		r.State = model.RouteLocked
		r.OpenedAt = now
	}

	// persist everything inside one transaction
	if err := s.persistRouteSetup(ctx, r, sig, anyMoving); err != nil {
		// rollback in-memory
		s.rtCtrl.UnlockAll(rg, r)
		sig.Aspect = model.AspectRed
		sig.Status = model.SignalSetRed
		sig.RouteID = ""
		return nil, err
	}
	s.routes[r.ID] = r

	aspect := sig.Aspect
	return &RouteResult{Route: r, Expanded: expanded, Cleared: r.State == model.RouteLocked, Aspect: aspect}, nil
}

// persistRouteSetup writes the route, its (moved) points, the (locked) sections
// and the (cleared) signal inside one transaction, plus audit events.
func (s *Service) persistRouteSetup(ctx context.Context, r *model.Route, sig *model.Signal, anyMoving bool) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.InsertRouteTx(ctx, tx, r); err != nil {
			return err
		}
		for _, sid := range r.PathSections {
			if sec, ok := s.graph.Section(sid); ok {
				if err := s.store.UpdateSectionTx(ctx, tx, sec); err != nil {
					return err
				}
			}
		}
		for _, pr := range r.PointsRequired {
			if p, ok := s.graph.Point(pr.PointID); ok {
				if err := s.store.UpdatePointTx(ctx, tx, p); err != nil {
					return err
				}
			}
		}
		for _, pr := range r.FlankProtection {
			if p, ok := s.graph.Point(pr.PointID); ok {
				if err := s.store.UpdatePointTx(ctx, tx, p); err != nil {
					return err
				}
			}
		}
		if err := s.store.UpdateSignalTx(ctx, tx, sig); err != nil {
			return err
		}
		if err := s.appendEventTx(ctx, tx, model.EventRouteRequest, mustJSON(r)); err != nil {
			return err
		}
		if anyMoving {
			for _, pr := range r.PointsRequired {
				if p, ok := s.graph.Point(pr.PointID); ok && p.Status == model.PointMoving {
					_ = s.appendEventTx(ctx, tx, model.EventPointMove, mustJSON(map[string]any{"point": p.ID, "dir": pr.Direction, "deadline": p.MoveDeadline}))
				}
			}
		} else {
			_ = s.appendEventTx(ctx, tx, model.EventSignalClear, mustJSON(map[string]any{"signal": sig.ID, "route": r.ID, "aspect": sig.Aspect}))
		}
		return nil
	})
}

// persistConflictRoute writes a CONFLICT route record + event.
func (s *Service) persistConflictRoute(ctx context.Context, r *model.Route) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.InsertRouteTx(ctx, tx, r); err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, model.EventRouteConflict, mustJSON(r))
	})
}

// persistRouteTx writes a single route row (used by Fail path).
func (s *Service) persistRouteTx(ctx context.Context, r *model.Route) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.InsertRouteTx(ctx, tx, r); err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, model.EventRouteRequest, mustJSON(r))
	})
}
