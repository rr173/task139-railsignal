package route

import (
	"testing"

	"task139-railsignal/internal/model"
)

// mockResourceGraph records section/point lock state for assertions.
type mockGraph struct {
	sectionLock map[string]string
	pointLock   map[string]string
	occupied    map[string]int
}

func newMockGraph() *mockGraph {
	return &mockGraph{sectionLock: map[string]string{}, pointLock: map[string]string{}, occupied: map[string]int{}}
}

func (m *mockGraph) SetSectionLock(sid, rid string) { m.sectionLock[sid] = rid }
func (m *mockGraph) SetPointLock(pid, rid string)   { m.pointLock[pid] = rid }
func (m *mockGraph) SectionOccupied(sid string) bool { return m.occupied[sid] > 0 }

func (m *mockGraph) occupy(sid string)  { m.occupied[sid]++ }
func (m *mockGraph) clear(sid string)    { if m.occupied[sid] > 0 { m.occupied[sid]-- } }

func straightRoute(id string) *model.Route {
	return &model.Route{
		ID:             id,
		State:          model.RouteLocked,
		PathSections:   []string{"s1", "s2", "s3"},
		ApproachSectionID: "approach",
		PointsRequired: []model.PointRequirement{{PointID: "pt1", Direction: model.DirNormal}},
		ReleasedCount:  0,
	}
}

func TestLockAndUnlockAll(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	if g.sectionLock["s1"] != "rt" || g.sectionLock["s2"] != "rt" || g.sectionLock["s3"] != "rt" {
		t.Fatalf("sections not locked: %+v", g.sectionLock)
	}
	if g.pointLock["pt1"] != "rt" {
		t.Fatalf("point not locked: %+v", g.pointLock)
	}
	m.UnlockAll(g, r)
	if g.sectionLock["s1"] != "" || g.pointLock["pt1"] != "" {
		t.Fatalf("resources not released: %+v / %+v", g.sectionLock, g.pointLock)
	}
	if r.ReleasedCount != 3 {
		t.Fatalf("released count = %d, want 3", r.ReleasedCount)
	}
}

func TestOnOccupancyApproachThenFirstSection(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	// approach occupied -> TRAIN_COMING
	g.occupy("approach")
	if st, _ := m.OnOccupancy(g, r, "approach", 0); st != model.RouteTrainComing {
		t.Fatalf("approach occupancy: state = %s, want TRAIN_COMING", st)
	}
	// first path section occupied -> TRAIN_IN_ROUTE
	g.occupy("s1")
	if st, _ := m.OnOccupancy(g, r, "s1", 0); st != model.RouteTrainInRoute {
		t.Fatalf("first-section occupancy: state = %s, want TRAIN_IN_ROUTE", st)
	}
}

func TestOnClearanceReleasesForwardAndFinal(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	r.State = model.RouteTrainInRoute
	// train occupies s1, s2, s3 in order
	g.occupy("s1")
	g.occupy("s2")
	g.occupy("s3")
	// clear s1 (rear leaves) then OnClearance -> release idx 0
	g.clear("s1")
	st, rel := m.OnClearance(g, r, "s1", 0)
	if !rel || st != model.RouteReleasing {
		t.Fatalf("clear s1: state=%s rel=%v, want RELEASING", st, rel)
	}
	if r.ReleasedCount != 1 || g.sectionLock["s1"] != "" {
		t.Fatalf("s1 not released: count=%d lock=%s", r.ReleasedCount, g.sectionLock["s1"])
	}
	// clear s2 (rear leaves) then release idx 1
	g.clear("s2")
	st, _ = m.OnClearance(g, r, "s2", 0)
	if r.ReleasedCount != 2 || g.sectionLock["s2"] != "" {
		t.Fatalf("s2 not released: count=%d lock=%s", r.ReleasedCount, g.sectionLock["s2"])
	}
	// clear s3 (train fully left) then release the last section
	g.clear("s3")
	st, _ = m.OnClearance(g, r, "s3", 0)
	if st != model.RouteReleased {
		t.Fatalf("clear s3: state=%s, want RELEASED", st)
	}
	if r.ReleasedCount != 3 {
		t.Fatalf("released count = %d, want 3", r.ReleasedCount)
	}
	if g.pointLock["pt1"] != "" {
		t.Fatalf("point not released on RELEASED: %s", g.pointLock["pt1"])
	}
}

func TestOnClearanceRefusesOutOfOrder(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	r.State = model.RouteTrainInRoute
	g.occupy("s1")
	g.occupy("s2")
	// trying to clear s2 before s1 is released -> refused
	st, rel := m.OnClearance(g, r, "s2", 0)
	if rel {
		t.Fatalf("out-of-order clearance should not release; state=%s", st)
	}
	if r.ReleasedCount != 0 {
		t.Fatalf("released count = %d, want 0", r.ReleasedCount)
	}
}

func TestCancelUnoccupiedRouteReleasesImmediately(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	// no train: first path section unoccupied.
	st, err := m.Cancel(g, r, 0)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if st != model.RouteCancelled {
		t.Fatalf("state = %s, want CANCELLED", st)
	}
	if g.sectionLock["s1"] != "" || g.pointLock["pt1"] != "" {
		t.Fatalf("resources should be released on cancel: %+v/%+v", g.sectionLock, g.pointLock)
	}
}

func TestCancelOccupiedRouteGoesPending(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	r.State = model.RouteTrainInRoute
	g.occupy("s1")
	st, err := m.Cancel(g, r, 0)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if st != model.RouteCancelPending {
		t.Fatalf("state = %s, want CANCEL_PENDING", st)
	}
	// resources stay locked.
	if g.sectionLock["s1"] != "rt" {
		t.Fatalf("section s1 should remain locked, got %q", g.sectionLock["s1"])
	}
	// timed release backstop after deadline and clear.
	g.clear("s1")
	r.CancelDeadline = 0
	st, _ = m.TickCancelDeadline(g, r, 10)
	if st != model.RouteCancelled {
		t.Fatalf("backstop release: state = %s, want CANCELLED", st)
	}
}

func TestTickCancelDeadlineWaitsForDeadline(t *testing.T) {
	m := New()
	g := newMockGraph()
	r := straightRoute("rt")
	m.LockResources(g, r)
	r.State = model.RouteCancelPending
	g.occupy("s1")
	r.CancelDeadline = 100
	if st, rel := m.TickCancelDeadline(g, r, 50); rel {
		t.Fatalf("should not release before deadline; state=%s", st)
	}
}
