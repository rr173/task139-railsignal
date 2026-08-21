package service

import (
	"context"

	"task139-railsignal/internal/interlocking"
	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// InterlockingReport is the GET /reports/interlocking response: the current
// yard state with each route's progress and the live conflict picture for any
// pending route that could be re-evaluated.
type InterlockingReport struct {
	Sections []*model.TrackSection `json:"sections"`
	Points   []*model.Point        `json:"points"`
	Signals  []*model.Signal       `json:"signals"`
	Routes   []*model.Route        `json:"routes"`
	Clock    int                   `json:"clock"`
}

// InterlockingReport returns the live interlocking state.
func (s *Service) InterlockingReport(ctx context.Context) *InterlockingReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &InterlockingReport{
		Sections: s.graph.Sections(),
		Points:   s.graph.Points(),
		Signals:  s.graph.Signals(),
		Routes:   s.allRoutes(),
		Clock:    s.now(),
	}
}

// ConflictReport re-evaluates, for each non-terminal route, what would conflict
// if it were re-requested against the current yard. Useful for the operator
// console to see why a route cannot be (re)established.
type ConflictReport struct {
	RouteID   string                `json:"route_id"`
	State     model.RouteState      `json:"state"`
	Conflicts []model.ConflictItem  `json:"conflicts"`
}

// ConflictReportAll re-evaluates conflicts for all routes' expansion.
func (s *Service) ConflictReportAll(ctx context.Context) []ConflictReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ConflictReport
	for _, r := range s.routes {
		expanded := &topology.ExpandedRoute{
			PathSections:     r.PathSections,
			PointsRequired:   r.PointsRequired,
			FlankProtection:  r.FlankProtection,
			ApproachSectionID: r.ApproachSectionID,
		}
		items := interlocking.Check(s.graph, s.activeRoutes(), expanded, r.ID)
		out = append(out, ConflictReport{RouteID: r.ID, State: r.State, Conflicts: items})
	}
	return out
}

// AuditReport is the GET /reports/audit response.
type AuditReport struct {
	Events []*model.Event `json:"events"`
}

// AuditReport returns recent events (paginated).
func (s *Service) AuditReport(ctx context.Context, limit, offset int) (*AuditReport, error) {
	events, err := s.store.ListEvents(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	return &AuditReport{Events: events}, nil
}

// GetRoute returns a single route.
func (s *Service) GetRoute(ctx context.Context, id string) (*model.Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.routes[id]
	if !ok {
		return nil, model.NotFoundf("route %s not found", id)
	}
	return r, nil
}

// ListRoutes returns routes optionally filtered by state.
func (s *Service) ListRoutes(ctx context.Context, state string) []*model.Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == "" {
		return s.allRoutes()
	}
	var out []*model.Route
	for _, r := range s.routes {
		if string(r.State) == state {
			out = append(out, r)
		}
	}
	return out
}

// GetSection returns a section by id.
func (s *Service) GetSection(ctx context.Context, id string) (*model.TrackSection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec, ok := s.graph.Section(id)
	if !ok {
		return nil, model.NotFoundf("section %s not found", id)
	}
	return sec, nil
}

// ListPoints returns all points.
func (s *Service) ListPoints(ctx context.Context) []*model.Point {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph.Points()
}

// ListSignals returns all signals.
func (s *Service) ListSignals(ctx context.Context) []*model.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph.Signals()
}

// ListSections returns all sections.
func (s *Service) ListSections(ctx context.Context) []*model.TrackSection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph.Sections()
}

// ListNodes returns all nodes.
func (s *Service) ListNodes(ctx context.Context) []*model.Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph.Nodes()
}

// GetPoint returns a point by id.
func (s *Service) GetPoint(ctx context.Context, id string) (*model.Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.graph.Point(id)
	if !ok {
		return nil, model.NotFoundf("point %s not found", id)
	}
	return p, nil
}

// GetSignal returns a signal by id.
func (s *Service) GetSignal(ctx context.Context, id string) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sig, ok := s.graph.Signal(id)
	if !ok {
		return nil, model.NotFoundf("signal %s not found", id)
	}
	return sig, nil
}

// Reconcile reloads the authoritative store into memory and re-derives all
// interlocking state (the restart path). Returns a summary of the reconciled
// counts.
type ReconcileResult struct {
	Nodes    int `json:"nodes"`
	Sections int `json:"sections"`
	Points   int `json:"points"`
	Signals  int `json:"signals"`
	Routes   int `json:"routes"`
	Clock    int `json:"clock"`
}

// Reconcile forces a full reload + ReconcileAll.
func (s *Service) Reconcile(ctx context.Context) (*ReconcileResult, error) {
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return &ReconcileResult{
		Nodes:    len(s.graph.Nodes()),
		Sections: len(s.graph.Sections()),
		Points:   len(s.graph.Points()),
		Signals:  len(s.graph.Signals()),
		Routes:   len(s.routes),
		Clock:    s.now(),
	}, nil
}
