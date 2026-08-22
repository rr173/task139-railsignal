package recovery

import (
	"context"
	"testing"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/store"
)

// buildAndSeed constructs a yard in a store with one active locked route, so the
// recovery tests can verify the recompute logic.
func buildAndSeed(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	mustNode(t, st, "nAP", "nJ1", "nJ2", "nA", "nB", "nT1", "nT2")
	mustSection(t, st, "sAP", model.KindBlock, "nAP", "nJ1")
	mustSection(t, st, "s1", model.KindLadder, "nJ1", "nJ2")
	mustSection(t, st, "s2n", model.KindLadder, "nJ2", "nA")
	mustSection(t, st, "s2r", model.KindLadder, "nJ2", "nB")
	mustSection(t, st, "trackA", model.KindTrack, "nA", "nT1")
	mustSection(t, st, "trackB", model.KindTrack, "nB", "nT2")
	mustPoint(t, st, "pt1", "nJ2", "s1", "s2n", "s2r", model.DirNormal)
	mustSignal(t, st, "sigA", "nJ1", "s1")
	// an active locked route sigA -> trackA, point normal, mid-release after s1.
	r := &model.Route{
		ID: "rt1", Code: "RT", OriginSignalID: "sigA", TerminalSectionID: "trackA",
		TerminalKind: model.KindTrack, State: model.RouteReleasing,
		PathSections:     []string{"s1", "s2n", "trackA"},
		PointsRequired:   []model.PointRequirement{{PointID: "pt1", Direction: model.DirNormal}},
		ApproachSectionID: "sAP", OpenedAt: 50, ReleasedCount: 1,
	}
	if err := st.InsertRoute(ctx, r); err != nil {
		t.Fatalf("insert route: %v", err)
	}
	// set runtime section/point state to match a mid-release snapshot.
	// s1 released (lock=""), s2n and trackA still locked by rt1.
	sec, _ := st.GetSection(ctx, "s1")
	sec.LockedByRoute = ""
	_ = st.UpdateSection(ctx, sec)
	sec2, _ := st.GetSection(ctx, "s2n")
	sec2.LockedByRoute = "rt1"
	_ = st.UpdateSection(ctx, sec2)
	sec3, _ := st.GetSection(ctx, "trackA")
	sec3.LockedByRoute = "rt1"
	_ = st.UpdateSection(ctx, sec3)
	pt, _ := st.GetPoint(ctx, "pt1")
	pt.LockedByRoute = "rt1"
	_ = st.UpdatePoint(ctx, pt)
	sig, _ := st.GetSignal(ctx, "sigA")
	sig.Aspect = model.AspectGreen
	sig.Status = model.SignalClearable
	sig.RouteID = "rt1"
	_ = st.UpdateSignal(ctx, sig)
	return st
}

func mustNode(t *testing.T, st *store.Store, ids ...string) {
	ctx := context.Background()
	for i, id := range ids {
		if err := st.InsertNode(ctx, &model.Node{ID: id, Code: id}); err != nil {
			t.Fatalf("insert node %d: %v", i, err)
		}
	}
}

func mustSection(t *testing.T, st *store.Store, id string, kind model.TrackSectionKind, from, to string) {
	ctx := context.Background()
	if err := st.InsertSection(ctx, &model.TrackSection{ID: id, Code: id, LengthM: 100, Kind: kind, FromNodeID: from, ToNodeID: to}); err != nil {
		t.Fatalf("insert section %s: %v", id, err)
	}
}

func mustPoint(t *testing.T, st *store.Store, id, node, heel, normal, reverse string, dir model.PointDirection) {
	ctx := context.Background()
	if err := st.InsertPoint(ctx, &model.Point{ID: id, Code: id, NodeID: node, HeelSectionID: heel, NormalSectionID: normal, ReverseSectionID: reverse, Direction: dir, Status: model.PointInPosition, MaxMoveSeconds: 5}); err != nil {
		t.Fatalf("insert point %s: %v", id, err)
	}
}

func mustSignal(t *testing.T, st *store.Store, id, entry, guard string) {
	ctx := context.Background()
	if err := st.InsertSignal(ctx, &model.Signal{ID: id, Code: id, EntryNodeID: entry, GuardSectionID: guard, Aspect: model.AspectRed, Status: model.SignalSetRed}); err != nil {
		t.Fatalf("insert signal %s: %v", id, err)
	}
}

func TestLoadAllRebuildsLayout(t *testing.T) {
	st := buildAndSeed(t)
	ctx := context.Background()
	snap, err := LoadAll(ctx, st)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snap.Graph.Nodes()) != 7 {
		t.Fatalf("nodes = %d, want 7", len(snap.Graph.Nodes()))
	}
	if len(snap.Graph.Sections()) != 6 {
		t.Fatalf("sections = %d, want 6", len(snap.Graph.Sections()))
	}
	if len(snap.Graph.Points()) != 1 {
		t.Fatalf("points = %d, want 1", len(snap.Graph.Points()))
	}
	if len(snap.Routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(snap.Routes))
	}
}

func TestReconcileRecomputesLocks(t *testing.T) {
	st := buildAndSeed(t)
	ctx := context.Background()
	snap, _ := LoadAll(ctx, st)
	// BEFORE reconcile: the graph from LoadAll has layout fields + persisted runtime
	// (since LoadAll overlays persisted occupancy/lock). Simulate a "lost locks"
	// scenario by clearing them, then reconcile should restore.
	for _, sec := range snap.Graph.Sections() {
		sec.LockedByRoute = ""
	}
	for _, p := range snap.Graph.Points() {
		p.LockedByRoute = ""
	}
	g, routes := ReconcileAll(snap)
	r := routes[0]
	// s2n and trackA should be re-locked (unreleased tail), s1 free.
	s2n, _ := g.Section("s2n")
	if s2n.LockedByRoute != "rt1" {
		t.Fatalf("s2n lock = %q, want rt1", s2n.LockedByRoute)
	}
	trackA, _ := g.Section("trackA")
	if trackA.LockedByRoute != "rt1" {
		t.Fatalf("trackA lock = %q, want rt1", trackA.LockedByRoute)
	}
	s1, _ := g.Section("s1")
	if s1.LockedByRoute != "" {
		t.Fatalf("s1 lock = %q, want '' (released)", s1.LockedByRoute)
	}
	pt, _ := g.Point("pt1")
	if pt.LockedByRoute != "rt1" {
		t.Fatalf("pt1 lock = %q, want rt1", pt.LockedByRoute)
	}
	if r.State != model.RouteReleasing {
		t.Fatalf("route state = %s, want RELEASING", r.State)
	}
}

func TestReconcileDropsSignalWhenConditionsFail(t *testing.T) {
	st := buildAndSeed(t)
	ctx := context.Background()
	snap, _ := LoadAll(ctx, st)
	// make a required point not in position -> signal should drop to RED.
	pt, _ := snap.Graph.Point("pt1")
	pt.Status = model.PointFault
	g, routes := ReconcileAll(snap)
	_ = routes
	sig, _ := g.Signal("sigA")
	if sig.Aspect != model.AspectRed {
		t.Fatalf("signal aspect = %s, want RED when point faulted", sig.Aspect)
	}
}

// TestReconcileDropsSignalWhenPathSectionOccupied guards the "恢复后的安全检查
// 也必须保持这一限制" invariant: after a restart, a route whose terminal
// track is occupied (压车) must have its origin signal dropped to RED, just as
// a fresh dispatch must not establish/open a route into an occupied terminal.
func TestReconcileDropsSignalWhenPathSectionOccupied(t *testing.T) {
	st := buildAndSeed(t)
	ctx := context.Background()
	snap, _ := LoadAll(ctx, st)
	// occupy the terminal track (trackA) of the active route rt1.
	trackA, _ := snap.Graph.Section("trackA")
	trackA.OccupancyCnt = 1
	g, routes := ReconcileAll(snap)
	_ = routes
	sig, _ := g.Signal("sigA")
	if sig.Aspect != model.AspectRed {
		t.Fatalf("signal aspect = %s, want RED when path section occupied", sig.Aspect)
	}
	if sig.Status != model.SignalSetRed {
		t.Fatalf("signal status = %s, want SET_RED when path section occupied", sig.Status)
	}
}

func TestReconcileIdempotent(t *testing.T) {
	st := buildAndSeed(t)
	ctx := context.Background()
	snap, _ := LoadAll(ctx, st)
	g1, r1 := ReconcileAll(snap)
	// second pass: build a fresh snap from the same store and reconcile again.
	snap2, _ := LoadAll(ctx, st)
	g2, r2 := ReconcileAll(snap2)
	if len(r1) != len(r2) {
		t.Fatalf("route count differs across reconciles: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].State != r2[i].State {
			t.Fatalf("route %d state differs: %s vs %s", i, r1[i].State, r2[i].State)
		}
		if r1[i].ReleasedCount != r2[i].ReleasedCount {
			t.Fatalf("route %d released count differs: %d vs %d", i, r1[i].ReleasedCount, r2[i].ReleasedCount)
		}
	}
	// section locks match
	for _, s := range g1.Sections() {
		s2, _ := g2.Section(s.ID)
		if s.LockedByRoute != s2.LockedByRoute {
			t.Fatalf("section %s lock differs: %q vs %q", s.ID, s.LockedByRoute, s2.LockedByRoute)
		}
	}
}
