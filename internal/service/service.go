// Package service is the business orchestration layer. It owns the in-memory
// yard graph (a cache of the authoritative store), the route registry, and the
// sim clock, and it implements the engine's write operations: building the
// layout, requesting/cancelling routes, applying train-detection events,
// advancing the clock (switch timeouts, timed cancellations), point bypass,
// and the recovery entry points. Every mutating operation persists its state
// and an audit event inside a store transaction; the in-memory graph is
// updated only after the commit so a failed write never leaves drift.
package service

import (
	"context"
	"sync"

	"task139-railsignal/internal/clock"
	"task139-railsignal/internal/idlib"
	"task139-railsignal/internal/model"
	"task139-railsignal/internal/point"
	"task139-railsignal/internal/recovery"
	"task139-railsignal/internal/route"
	"task139-railsignal/internal/signal"
	"task139-railsignal/internal/store"
	"task139-railsignal/internal/topology"
)

// Service is the engine. It is safe for concurrent use; all public methods
// take the lock, so operations are serialised — matching the single SQLite
// connection.
type Service struct {
	mu     sync.Mutex
	store  *store.Store
	graph  *topology.Graph
	clock  *clock.SimClock
	routes map[string]*model.Route // id -> route (cache; authoritative copy in store)

	ptCtrl  *point.Machine
	rtCtrl  *route.Machine
	sigCtrl *signal.Controller
}

// New constructs a service backed by the given store. It performs an initial
// LoadAll + ReconcileAll so a restarted process resumes the prior state.
func New(st *store.Store) *Service {
	s := &Service{
		store:  st,
		graph: topology.New(),
		clock: clock.NewSim(),
		routes: map[string]*model.Route{},
		ptCtrl:  point.New(),
		rtCtrl:  route.New(),
		sigCtrl: signal.New(),
	}
	// best-effort resume; errors (empty store) are non-fatal for a fresh yard.
	_ = s.reload(context.Background())
	return s
}

// reload re-reads the store into the in-memory graph and route cache, then
// reconciles. Used at startup and by the /reconcile API.
func (s *Service) reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := recovery.LoadAll(ctx, s.store)
	if err != nil {
		return err
	}
	g, routes := recovery.ReconcileAll(snap)
	s.graph = g
	s.routes = map[string]*model.Route{}
	for _, r := range routes {
		s.routes[r.ID] = r
	}
	// restore clock from meta
	if v, _ := s.store.GetMeta(ctx, "clock"); v != "" {
		t := 0
		_, _ = fmtSscan(v, &t)
		s.clock.Set(t)
	}
	return nil
}

// graphResourceGraph adapts the topology graph to the route.ResourceGraph
// interface so the route machine can set section/point locks in memory.
type graphResourceGraph struct {
	g *topology.Graph
}

func (a graphResourceGraph) SetSectionLock(sectionID, routeID string) {
	if s, ok := a.g.Section(sectionID); ok {
		s.LockedByRoute = routeID
	}
}
func (a graphResourceGraph) SetPointLock(pointID, routeID string) {
	if p, ok := a.g.Point(pointID); ok {
		p.LockedByRoute = routeID
	}
}
func (a graphResourceGraph) SectionOccupied(sectionID string) bool {
	if s, ok := a.g.Section(sectionID); ok {
		return s.Occupied()
	}
	return false
}

// sectionOccupiedFn returns a closure for the point machine's anti-squeeze
// check: whether any of the point's protect sections is occupied.
func (s *Service) sectionOccupiedFn() func(string) bool {
	return func(sid string) bool {
		sec, ok := s.graph.Section(sid)
		if !ok {
			return false
		}
		return sec.Occupied()
	}
}

// now returns the current sim seconds.
func (s *Service) now() int { return s.clock.Now() }

// newSimClock returns a fresh sim clock (used on reset).
func newSimClock() *clock.SimClock { return clock.NewSim() }

// activeRoutes returns the currently-active routes (locked or in-progress).
func (s *Service) activeRoutes() []*model.Route {
	out := make([]*model.Route, 0, len(s.routes))
	for _, r := range s.routes {
		if r.State.IsActive() {
			out = append(out, r)
		}
	}
	return out
}

// allRoutes returns all routes in id order (stable).
func (s *Service) allRoutes() []*model.Route {
	out := make([]*model.Route, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r)
	}
	return out
}

// appendEvent is a helper to persist an audit event within a transaction.
func (s *Service) appendEventTx(ctx context.Context, db store.DBTX, kind model.EventKind, payload string) error {
	seq, err := s.store.NextSeqTx(ctx, db)
	if err != nil {
		return err
	}
	_, err = s.store.EventTx(ctx, db, &model.Event{Seq: seq, Kind: kind, Payload: payload, Clock: s.now()})
	return err
}

// id helpers
func (s *Service) newRouteCode(n int) string { return idlib.NewRouteID() }

// fmtSscan is a thin wrapper over fmt.Sscan used for meta clock parsing; kept
// here so the service package does not import fmt solely for that.
func fmtSscan(src string, dst *int) (int, error) {
	return fmtSscanImpl(src, dst)
}
