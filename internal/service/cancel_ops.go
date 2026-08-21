package service

import (
	"context"
	"database/sql"

	"task139-railsignal/internal/model"
)

// CancelRoute requests cancellation. If the route has no train in it, it
// cancels and releases immediately. If a train is present, it goes to
// CANCEL_PENDING (rear-of-train protection).
func (s *Service) CancelRoute(ctx context.Context, routeID string) (*model.Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.routes[routeID]
	if !ok {
		return nil, model.NotFoundf("route %s not found", routeID)
	}
	rg := graphResourceGraph{g: s.graph}
	prev := r.State
	newState, err := s.rtCtrl.Cancel(rg, r, s.now())
	if err != nil {
		return nil, err
	}
	// if cancel pending, set a backstop deadline based on the route's transit
	// time + margin (rear-of-train protection: no release before the train
	// could have cleared the path).
	if newState == model.RouteCancelPending {
		if r.OpenedAt > 0 && r.TransitSec > 0 {
			r.CancelDeadline = s.now() + r.TransitSec + 30
		} else {
			r.CancelDeadline = s.now() + 120
		}
	}
	// drop the signal to RED on cancel
	if sig, ok := s.graph.Signal(r.OriginSignalID); ok {
		sig.Aspect = model.AspectRed
		sig.Status = model.SignalSetRed
		if newState == model.RouteCancelled || newState == model.RouteCancelPending {
			sig.RouteID = ""
		}
		if err := s.persistCancel(ctx, r, sig); err != nil {
			r.State = prev
			sig.Aspect = model.AspectRed
			return nil, err
		}
	} else {
		if err := s.persistCancelRouteOnly(ctx, r); err != nil {
			r.State = prev
			return nil, err
		}
	}
	// if cancelled (released), update released sections/points in store
	if newState == model.RouteCancelled {
		_ = s.persistCancelReleases(ctx, r)
	}
	return r, nil
}

func (s *Service) persistCancel(ctx context.Context, r *model.Route, sig *model.Signal) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.UpdateRouteTx(ctx, tx, r); err != nil {
			return err
		}
		if err := s.store.UpdateSignalTx(ctx, tx, sig); err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, model.EventRouteCancel, mustJSON(map[string]any{"route": r.ID, "state": r.State}))
	})
}

func (s *Service) persistCancelRouteOnly(ctx context.Context, r *model.Route) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.UpdateRouteTx(ctx, tx, r); err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, model.EventRouteCancel, mustJSON(map[string]any{"route": r.ID, "state": r.State}))
	})
}

func (s *Service) persistCancelReleases(ctx context.Context, r *model.Route) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
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
		return nil
	})
}

// BypassPoint toggles degraded-mode bypass on a point. Refused if the point's
// anti-squeeze section is occupied or the point is healthy.
func (s *Service) BypassPoint(ctx context.Context, pointID string, on bool) (*model.Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.graph.Point(pointID)
	if !ok {
		return nil, model.NotFoundf("point %s not found", pointID)
	}
	if err := s.ptCtrl.Bypass(p, on, s.sectionOccupiedFn()); err != nil {
		return nil, err
	}
	if err := s.persistPoint(ctx, p, model.EventPointBypass); err != nil {
		// rollback
		_ = s.ptCtrl.Bypass(p, !on, s.sectionOccupiedFn())
		return nil, err
	}
	return p, nil
}

// OperatePoint manually drives a point to a direction (only when free / not
// locked by an active route and not under a train).
func (s *Service) OperatePoint(ctx context.Context, pointID string, dir model.PointDirection) (*model.Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.graph.Point(pointID)
	if !ok {
		return nil, model.NotFoundf("point %s not found", pointID)
	}
	if p.LockedByRoute != "" {
		return nil, model.Conflictf("point %s locked by route %s", p.ID, p.LockedByRoute)
	}
	if ok, err := s.ptCtrl.CanMove(p, dir, func(string) bool { return false }); err != nil {
		return nil, err
	} else if !ok {
		return p, nil // already at target
	}
	s.ptCtrl.IssueMove(p, dir, s.now())
	if err := s.persistPoint(ctx, p, model.EventPointMove); err != nil {
		return nil, err
	}
	return p, nil
}

// SetSignalRed manually forces a signal to RED (operator action).
func (s *Service) SetSignalRed(ctx context.Context, signalID string) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sig, ok := s.graph.Signal(signalID)
	if !ok {
		return nil, model.NotFoundf("signal %s not found", signalID)
	}
	sig.Aspect = model.AspectRed
	sig.Status = model.SignalSetRed
	// persist via dedicated method below
	if err := s.persistSignal(ctx, sig, model.EventSignalSetRed); err != nil {
		return nil, err
	}
	return sig, nil
}

func (s *Service) persistPoint(ctx context.Context, p *model.Point, evt model.EventKind) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.UpdatePointTx(ctx, tx, p); err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, evt, mustJSON(map[string]any{"point": p.ID, "status": p.Status, "dir": p.Direction, "bypassed": p.Bypassed}))
	})
}

func (s *Service) persistSignal(ctx context.Context, sig *model.Signal, evt model.EventKind) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.UpdateSignalTx(ctx, tx, sig); err != nil {
			return err
		}
		return s.appendEventTx(ctx, tx, evt, mustJSON(map[string]any{"signal": sig.ID, "aspect": sig.Aspect, "route": sig.RouteID}))
	})
}
