package interlocking

import (
	"testing"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// buildYard mirrors topology_test's buildSimpleYard but in-package, so the
// interlocking tests can drive it directly without the HTTP layer.
func buildYard(t *testing.T) *topology.Graph {
	t.Helper()
	g := topology.New()
	adds := []func() error{
		func() error { return g.AddNode(&model.Node{ID: "nAP", Code: "N-AP"}) },
		func() error { return g.AddNode(&model.Node{ID: "nJ1", Code: "N-J1"}) },
		func() error { return g.AddNode(&model.Node{ID: "nJ2", Code: "N-J2"}) },
		func() error { return g.AddNode(&model.Node{ID: "nA", Code: "N-A"}) },
		func() error { return g.AddNode(&model.Node{ID: "nB", Code: "N-B"}) },
		func() error { return g.AddNode(&model.Node{ID: "nT1", Code: "N-T1"}) },
		func() error { return g.AddNode(&model.Node{ID: "nT2", Code: "N-T2"}) },
		func() error {
			return g.AddSection(&model.TrackSection{ID: "sAP", Code: "SEC-AP", LengthM: 100, Kind: model.KindBlock, FromNodeID: "nAP", ToNodeID: "nJ1"})
		},
		func() error {
			return g.AddSection(&model.TrackSection{ID: "s1", Code: "SEC-S1", LengthM: 80, Kind: model.KindLadder, FromNodeID: "nJ1", ToNodeID: "nJ2"})
		},
		func() error {
			return g.AddSection(&model.TrackSection{ID: "s2n", Code: "SEC-S2N", LengthM: 60, Kind: model.KindLadder, FromNodeID: "nJ2", ToNodeID: "nA"})
		},
		func() error {
			return g.AddSection(&model.TrackSection{ID: "s2r", Code: "SEC-S2R", LengthM: 60, Kind: model.KindLadder, FromNodeID: "nJ2", ToNodeID: "nB"})
		},
		func() error {
			return g.AddSection(&model.TrackSection{ID: "trackA", Code: "TA", LengthM: 200, Kind: model.KindTrack, FromNodeID: "nA", ToNodeID: "nT1"})
		},
		func() error {
			return g.AddSection(&model.TrackSection{ID: "trackB", Code: "TB", LengthM: 200, Kind: model.KindTrack, FromNodeID: "nB", ToNodeID: "nT2"})
		},
		func() error {
			return g.AddPoint(&model.Point{ID: "pt1", Code: "PT1", NodeID: "nJ2", HeelSectionID: "s1", NormalSectionID: "s2n", ReverseSectionID: "s2r", Direction: model.DirNormal, MaxMoveSeconds: 5})
		},
		func() error {
			return g.AddSignal(&model.Signal{ID: "sigA", Code: "SIG-A", EntryNodeID: "nJ1", GuardSectionID: "s1"})
		},
	}
	for _, a := range adds {
		if err := a(); err != nil {
			t.Fatalf("build: %v", err)
		}
	}
	return g
}

func TestCheckClearRoute(t *testing.T) {
	g := buildYard(t)
	expanded, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	items := Check(g, nil, expanded, "rt-new")
	if len(items) != 0 {
		t.Fatalf("expected no conflicts, got %+v", items)
	}
}

func TestCheckTerminalOccupied(t *testing.T) {
	g := buildYard(t)
	expanded, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// occupy trackA (terminal).
	sec, _ := g.Section("trackA")
	sec.OccupancyCnt = 1
	items := Check(g, nil, expanded, "rt-new")
	found := false
	for _, it := range items {
		if it.Kind == CTerminalOccupied {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TERMINAL_OCCUPIED conflict, got %+v", items)
	}
}

func TestCheckPathSectionOccupied(t *testing.T) {
	g := buildYard(t)
	expanded, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// occupy a mid path section.
	sec, _ := g.Section("s2n")
	sec.OccupancyCnt = 1
	items := Check(g, nil, expanded, "rt-new")
	found := false
	for _, it := range items {
		if it.Kind == CSectionOccupied {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SECTION_OCCUPIED conflict, got %+v", items)
	}
}

func TestCheckPathSectionLockedByActiveRoute(t *testing.T) {
	g := buildYard(t)
	expanded, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// simulate another active route locking s2n.
	existing := &model.Route{
		ID:            "rt-other",
		State:         model.RouteLocked,
		PathSections:  []string{"s2n"},
		PointsRequired: []model.PointRequirement{},
	}
	sec, _ := g.Section("s2n")
	sec.LockedByRoute = "rt-other"
	items := Check(g, []*model.Route{existing}, expanded, "rt-new")
	found := false
	for _, it := range items {
		if it.Kind == CSectionLocked {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SECTION_LOCKED conflict, got %+v", items)
	}
}

func TestCheckPointConflictWhenLockedOtherDirection(t *testing.T) {
	g := buildYard(t)
	expanded, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// another route locks pt1 to reverse.
	existing := &model.Route{
		ID:            "rt-other",
		State:         model.RouteLocked,
		PathSections:  []string{"s2r", "trackB"},
		PointsRequired: []model.PointRequirement{{PointID: "pt1", Direction: model.DirReverse}},
	}
	pt, _ := g.Point("pt1")
	pt.LockedByRoute = "rt-other"
	items := Check(g, []*model.Route{existing}, expanded, "rt-new")
	found := false
	for _, it := range items {
		if it.Kind == CPointConflict {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected POINT_CONFLICT, got %+v", items)
	}
}

func TestCheckPointFaultConflict(t *testing.T) {
	g := buildYard(t)
	expanded, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	pt, _ := g.Point("pt1")
	pt.Status = model.PointFault
	items := Check(g, nil, expanded, "rt-new")
	found := false
	for _, it := range items {
		if it.Kind == CPointConflict {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected POINT_CONFLICT for faulted point, got %+v", items)
	}
	// bypassing the faulted point removes the conflict.
	pt.Bypassed = true
	pt.Status = model.PointInPosition
	items = Check(g, nil, expanded, "rt-new")
	for _, it := range items {
		if it.Kind == CPointConflict {
			t.Fatalf("bypassed faulted point should not conflict, got %+v", it)
		}
	}
}

func TestIsActive(t *testing.T) {
	cases := []struct {
		state model.RouteState
		want  bool
	}{
		{model.RoutePending, false},
		{model.RoutePointsMoving, true},
		{model.RouteLocked, true},
		{model.RouteTrainComing, true},
		{model.RouteTrainInRoute, true},
		{model.RouteReleasing, true},
		{model.RouteCancelPending, true},
		{model.RouteReleased, false},
		{model.RouteCancelled, false},
		{model.RouteConflict, false},
		{model.RouteFailed, false},
	}
	for _, c := range cases {
		r := &model.Route{State: c.state}
		if got := IsActive(r); got != c.want {
			t.Errorf("IsActive(%s) = %v, want %v", c.state, got, c.want)
		}
	}
}
