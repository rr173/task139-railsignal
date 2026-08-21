package service

import (
	"context"
	"encoding/json"
	"strings"

	"task139-railsignal/internal/idlib"
	"task139-railsignal/internal/model"
)

// AddNodeRequest creates a node.
type AddNodeRequest struct {
	Code string `json:"code"`
}

// AddNode persists a node and updates the graph.
func (s *Service) AddNode(ctx context.Context, req AddNodeRequest) (*model.Node, error) {
	if strings.TrimSpace(req.Code) == "" {
		return nil, model.Invariantf("node code is empty")
	}
	n := &model.Node{ID: idlib.NewNodeID(), Code: req.Code}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.graph.AddNode(n); err != nil {
		return nil, err
	}
	if err := s.store.InsertNode(ctx, n); err != nil {
		// rollback in-memory
		s.graph.RemoveNode(n.ID)
		return nil, err
	}
	if err := s.appendEvent(ctx, model.EventNodeAdd, mustJSON(n)); err != nil {
		return nil, err
	}
	return n, nil
}

// appendEvent appends an audit event on the pool connection (outside any tx).
func (s *Service) appendEvent(ctx context.Context, kind model.EventKind, payload string) error {
	seq, err := s.store.NextEventSeq(ctx)
	if err != nil {
		return err
	}
	_, err = s.store.AppendEvent(ctx, &model.Event{Seq: seq, Kind: kind, Payload: payload, Clock: s.now()})
	return err
}

// AddSectionRequest creates a track section.
type AddSectionRequest struct {
	Code       string                  `json:"code"`
	LengthM    int                     `json:"length_m"`
	Kind       model.TrackSectionKind  `json:"kind"`
	FromNodeID string                  `json:"from_node_id"`
	ToNodeID   string                  `json:"to_node_id"`
}

// AddSection persists a section.
func (s *Service) AddSection(ctx context.Context, req AddSectionRequest) (*model.TrackSection, error) {
	if strings.TrimSpace(req.Code) == "" {
		return nil, model.Invariantf("section code is empty")
	}
	if req.Kind == "" {
		req.Kind = model.KindTrack
	}
	sec := &model.TrackSection{
		ID:         idlib.NewSectionID(),
		Code:       req.Code,
		LengthM:    req.LengthM,
		Kind:       req.Kind,
		FromNodeID: req.FromNodeID,
		ToNodeID:   req.ToNodeID,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.graph.AddSection(sec); err != nil {
		return nil, err
	}
	if err := s.store.InsertSection(ctx, sec); err != nil {
		s.graph.RemoveSection(sec.ID)
		return nil, err
	}
	if err := s.appendEvent(ctx, model.EventSectionAdd, mustJSON(sec)); err != nil {
		return nil, err
	}
	return sec, nil
}

// AddPointRequest creates a switch.
type AddPointRequest struct {
	Code            string                `json:"code"`
	NodeID          string                `json:"node_id"`
	HeelSectionID  string                `json:"heel_section_id"`
	NormalSectionID string               `json:"normal_section_id"`
	ReverseSectionID string               `json:"reverse_section_id"`
	Direction      model.PointDirection  `json:"direction"`
	MaxMoveSeconds int                   `json:"max_move_seconds"`
	ProtectSections []string             `json:"protect_sections"`
}

// AddPoint persists a switch.
func (s *Service) AddPoint(ctx context.Context, req AddPointRequest) (*model.Point, error) {
	if strings.TrimSpace(req.Code) == "" {
		return nil, model.Invariantf("point code is empty")
	}
	if req.Direction == "" {
		req.Direction = model.DirNormal
	}
	if req.MaxMoveSeconds <= 0 {
		req.MaxMoveSeconds = 10
	}
	p := &model.Point{
		ID:              idlib.NewPointID(),
		Code:            req.Code,
		NodeID:          req.NodeID,
		HeelSectionID:   req.HeelSectionID,
		NormalSectionID: req.NormalSectionID,
		ReverseSectionID: req.ReverseSectionID,
		Direction:       req.Direction,
		Status:          model.PointInPosition,
		MaxMoveSeconds:  req.MaxMoveSeconds,
		ProtectSections: req.ProtectSections,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.graph.AddPoint(p); err != nil {
		return nil, err
	}
	if err := s.store.InsertPoint(ctx, p); err != nil {
		s.graph.RemovePoint(p.ID)
		return nil, err
	}
	if err := s.appendEvent(ctx, model.EventPointAdd, mustJSON(p)); err != nil {
		return nil, err
	}
	return p, nil
}

// AddSignalRequest creates a signal.
type AddSignalRequest struct {
	Code           string `json:"code"`
	EntryNodeID    string `json:"entry_node_id"`
	GuardSectionID string `json:"guard_section_id"`
}

// AddSignal persists a signal.
func (s *Service) AddSignal(ctx context.Context, req AddSignalRequest) (*model.Signal, error) {
	if strings.TrimSpace(req.Code) == "" {
		return nil, model.Invariantf("signal code is empty")
	}
	sig := &model.Signal{
		ID:             idlib.NewSignalID(),
		Code:           req.Code,
		EntryNodeID:    req.EntryNodeID,
		GuardSectionID: req.GuardSectionID,
		Aspect:         model.AspectRed,
		Status:         model.SignalSetRed,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.graph.AddSignal(sig); err != nil {
		return nil, err
	}
	if err := s.store.InsertSignal(ctx, sig); err != nil {
		s.graph.RemoveSignal(sig.ID)
		return nil, err
	}
	if err := s.appendEvent(ctx, model.EventSignalAdd, mustJSON(sig)); err != nil {
		return nil, err
	}
	return sig, nil
}

// LayoutSnapshot is the GET /layout response.
type LayoutSnapshot struct {
	Nodes   []*model.Node           `json:"nodes"`
	Sections []*model.TrackSection `json:"sections"`
	Points  []*model.Point          `json:"points"`
	Signals []*model.Signal         `json:"signals"`
	Clock   int                     `json:"clock"`
}

// Layout returns the current yard snapshot.
func (s *Service) Layout(ctx context.Context) *LayoutSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &LayoutSnapshot{
		Nodes:   s.graph.Nodes(),
		Sections: s.graph.Sections(),
		Points:  s.graph.Points(),
		Signals: s.graph.Signals(),
		Clock:   s.now(),
	}
}

// ResetLayout clears the entire yard (debug/smoke-test).
func (s *Service) ResetLayout(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ResetDBTx(ctx); err != nil {
		return err
	}
	s.graph.Reset()
	s.routes = map[string]*model.Route{}
	s.clock = newSimClock()
	return nil
}

// mustJSON marshals v or returns "{}" on error (never panics).
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
