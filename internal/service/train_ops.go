package service

import (
	"context"
	"database/sql"

	"task139-railsignal/internal/model"
)

// OccupancyEvent raises a track-section occupancy (train entering).
type OccupancyEvent struct {
	SectionID string `json:"section_id"`
}

// ClearanceEvent raises a track-section clearance (train leaving).
type ClearanceEvent struct {
	SectionID string `json:"section_id"`
}

// TrainMoveResult summarises the state transitions caused by an occupancy or
// clearance event, so the caller can render the route-release progress.
type TrainMoveResult struct {
	SectionID   string            `json:"section_id"`
	Occupancy   int               `json:"occupancy_count"`
	Transitions []RouteTransition `json:"transitions"`
	Signals     []*model.Signal   `json:"signals"`
}

// RouteTransition is a single route state change caused by a train move.
type RouteTransition struct {
	RouteID       string           `json:"route_id"`
	From          model.RouteState `json:"from"`
	To            model.RouteState `json:"to"`
	ReleasedIndex int              `json:"released_index"`
}

// ReportOccupancy increments a section's occupancy and advances any route that
// owns the section or approach. It returns the transitions that occurred.
func (s *Service) ReportOccupancy(ctx context.Context, req OccupancyEvent) (*TrainMoveResult, error) {
	if req.SectionID == "" {
		return nil, model.Invariantf("section_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sec, ok := s.graph.Section(req.SectionID)
	if !ok {
		return nil, model.NotFoundf("section %s not found", req.SectionID)
	}
	sec.OccupancyCnt++
	now := s.now()
	rg := graphResourceGraph{g: s.graph}

	var trans []RouteTransition
	var touchedSignals []*model.Signal
	for _, r := range s.routes {
		if !r.State.IsActive() {
			continue
		}
		prev := r.State
		if _, changed := s.rtCtrl.OnOccupancy(rg, r, req.SectionID, now); changed {
			trans = append(trans, RouteTransition{RouteID: r.ID, From: prev, To: r.State})
		}
	}
	// a signal may need to drop to red if its first path section is now occupied.
	for _, r := range s.routes {
		if !r.State.IsActive() {
			continue
		}
		if r.PathSections[0] == req.SectionID {
			if sig, ok := s.graph.Signal(r.OriginSignalID); ok {
				sig.Aspect = model.AspectRed
				sig.Status = model.SignalSetRed
				touchedSignals = append(touchedSignals, sig)
			}
		}
	}

	// persist
	if err := s.persistTrainMove(ctx, sec, trans, touchedSignals); err != nil {
		// rollback in-memory
		sec.OccupancyCnt--
		for _, t := range trans {
			if r, ok := s.routes[t.RouteID]; ok {
				r.State = t.From // best-effort rollback
			}
		}
		return nil, err
	}
	return &TrainMoveResult{SectionID: req.SectionID, Occupancy: sec.OccupancyCnt, Transitions: trans, Signals: touchedSignals}, nil
}

// ReportClearance decrements a section's occupancy (floor at 0) and advances
// route-release: sections cleared by the train are released in forward order.
func (s *Service) ReportClearance(ctx context.Context, req ClearanceEvent) (*TrainMoveResult, error) {
	if req.SectionID == "" {
		return nil, model.Invariantf("section_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	sec, ok := s.graph.Section(req.SectionID)
	if !ok {
		return nil, model.NotFoundf("section %s not found", req.SectionID)
	}
	if sec.OccupancyCnt > 0 {
		sec.OccupancyCnt--
	}
	now := s.now()
	rg := graphResourceGraph{g: s.graph}

	var trans []RouteTransition
	var releasedSections []*model.TrackSection
	var releasedPoints []*model.Point
	var touchedSignals []*model.Signal
	seenSig := map[string]bool{}
	addSignal := func(sig *model.Signal) {
		if sig == nil || seenSig[sig.ID] {
			return
		}
		seenSig[sig.ID] = true
		touchedSignals = append(touchedSignals, sig)
	}
	for _, r := range s.routes {
		if !r.State.IsActive() {
			continue
		}
		prev := r.State
		newState, changed := s.rtCtrl.OnClearance(rg, r, req.SectionID, now)
		if changed {
			trans = append(trans, RouteTransition{RouteID: r.ID, From: prev, To: newState, ReleasedIndex: r.ReleasedCount - 1})
			// track released section for persistence
			releasedSections = append(releasedSections, sec)
			// if route became terminal, the points were released too — persist
			// the unlocked points so the resources are reusable after a restart.
			if newState.IsTerminal() {
				for _, pr := range r.PointsRequired {
					if p, ok := s.graph.Point(pr.PointID); ok {
						releasedPoints = append(releasedPoints, p)
					}
				}
				for _, pr := range r.FlankProtection {
					if p, ok := s.graph.Point(pr.PointID); ok {
						releasedPoints = append(releasedPoints, p)
					}
				}
				// drop the origin signal to RED and free it so the same route
				// may be re-established once the train has fully left.
				if r.OriginSignalID != "" {
					if sig, ok := s.graph.Signal(r.OriginSignalID); ok {
						sig.Aspect = model.AspectRed
						sig.Status = model.SignalSetRed
						sig.RouteID = ""
						addSignal(sig)
					}
				}
			}
		}
	}
	// after release, re-evaluate whether the origin signal may be re-cleared
	// (it normally stays RED until a new route is set, so this is a no-op here,
	// but we surface the signal state for the response).
	for _, r := range s.routes {
		if r.State == model.RouteLocked && r.OriginSignalID != "" {
			if sig, ok := s.graph.Signal(r.OriginSignalID); ok {
				addSignal(sig)
			}
		}
	}

	if err := s.persistClearance(ctx, sec, trans, releasedSections, releasedPoints, touchedSignals); err != nil {
		sec.OccupancyCnt++ // rollback
		return nil, err
	}
	return &TrainMoveResult{SectionID: req.SectionID, Occupancy: sec.OccupancyCnt, Transitions: trans, Signals: touchedSignals}, nil
}

// persistTrainMove writes the section + routes + signals inside one transaction.
func (s *Service) persistTrainMove(ctx context.Context, sec *model.TrackSection, trans []RouteTransition, sigs []*model.Signal) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.UpdateSectionTx(ctx, tx, sec); err != nil {
			return err
		}
		for _, t := range trans {
			if r, ok := s.routes[t.RouteID]; ok {
				if err := s.store.UpdateRouteTx(ctx, tx, r); err != nil {
					return err
				}
			}
		}
		for _, sig := range sigs {
			if err := s.store.UpdateSignalTx(ctx, tx, sig); err != nil {
				return err
			}
		}
		return s.appendEventTx(ctx, tx, model.EventOccupancy, mustJSON(map[string]any{"section": sec.ID, "occupancy": sec.OccupancyCnt}))
	})
}

// persistClearance writes section + route + point + signal changes after a
// clearance event.
func (s *Service) persistClearance(ctx context.Context, sec *model.TrackSection, trans []RouteTransition, releasedSec []*model.TrackSection, releasedPts []*model.Point, sigs []*model.Signal) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.UpdateSectionTx(ctx, tx, sec); err != nil {
			return err
		}
		for _, rsec := range releasedSec {
			if err := s.store.UpdateSectionTx(ctx, tx, rsec); err != nil {
				return err
			}
		}
		for _, p := range releasedPts {
			if err := s.store.UpdatePointTx(ctx, tx, p); err != nil {
				return err
			}
		}
		for _, t := range trans {
			if r, ok := s.routes[t.RouteID]; ok {
				if err := s.store.UpdateRouteTx(ctx, tx, r); err != nil {
					return err
				}
				if r.State.IsTerminal() {
					_ = s.appendEventTx(ctx, tx, model.EventRouteRelease, mustJSON(map[string]any{"route": r.ID, "state": r.State}))
				}
			}
		}
		for _, sig := range sigs {
			if err := s.store.UpdateSignalTx(ctx, tx, sig); err != nil {
				return err
			}
		}
		return s.appendEventTx(ctx, tx, model.EventClearance, mustJSON(map[string]any{"section": sec.ID, "occupancy": sec.OccupancyCnt}))
	})
}
