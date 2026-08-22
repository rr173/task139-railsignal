package signal

import (
	"testing"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// buildDivergingYard constructs a yard with a facing point whose reverse leg
// leads to a terminal, so a route to the terminal is a diverging route. The
// point starts detected REVERSE so the diverging route is immediately clearable.
//
//	nAP --J1-- s1 --J2(pt1)--> normal: s2n -> nA -> trackA
//	                         \-> reverse: s2r -> nB -> trackB  (terminal)
func buildDivergingYard(t *testing.T) *topology.Graph {
	t.Helper()
	g := topology.New()
	add := func(fn func() error) {
		t.Helper()
		if err := fn(); err != nil {
			t.Fatalf("build: %v", err)
		}
	}
	add(func() error { return g.AddNode(&model.Node{ID: "nAP", Code: "N-AP"}) })
	add(func() error { return g.AddNode(&model.Node{ID: "nJ1", Code: "N-J1"}) })
	add(func() error { return g.AddNode(&model.Node{ID: "nJ2", Code: "N-J2"}) })
	add(func() error { return g.AddNode(&model.Node{ID: "nA", Code: "N-A"}) })
	add(func() error { return g.AddNode(&model.Node{ID: "nB", Code: "N-B"}) })
	add(func() error { return g.AddNode(&model.Node{ID: "nT1", Code: "N-T1"}) })
	add(func() error { return g.AddNode(&model.Node{ID: "nT2", Code: "N-T2"}) })
	add(func() error {
		return g.AddSection(&model.TrackSection{ID: "sAP", Code: "SEC-AP", LengthM: 100, Kind: model.KindBlock, FromNodeID: "nAP", ToNodeID: "nJ1"})
	})
	add(func() error {
		return g.AddSection(&model.TrackSection{ID: "s1", Code: "SEC-S1", LengthM: 80, Kind: model.KindLadder, FromNodeID: "nJ1", ToNodeID: "nJ2"})
	})
	add(func() error {
		return g.AddSection(&model.TrackSection{ID: "s2n", Code: "SEC-S2N", LengthM: 60, Kind: model.KindLadder, FromNodeID: "nJ2", ToNodeID: "nA"})
	})
	add(func() error {
		return g.AddSection(&model.TrackSection{ID: "s2r", Code: "SEC-S2R", LengthM: 60, Kind: model.KindLadder, FromNodeID: "nJ2", ToNodeID: "nB"})
	})
	add(func() error {
		return g.AddSection(&model.TrackSection{ID: "trackA", Code: "SEC-TRACKA", LengthM: 200, Kind: model.KindTrack, FromNodeID: "nA", ToNodeID: "nT1"})
	})
	add(func() error {
		return g.AddSection(&model.TrackSection{ID: "trackB", Code: "SEC-TRACKB", LengthM: 200, Kind: model.KindTrack, FromNodeID: "nB", ToNodeID: "nT2"})
	})
	// point detected at REVERSE -> the reverse leg is the diverging route.
	add(func() error {
		return g.AddPoint(&model.Point{ID: "pt1", Code: "PT1", NodeID: "nJ2", HeelSectionID: "s1", NormalSectionID: "s2n", ReverseSectionID: "s2r", Direction: model.DirReverse, Status: model.PointInPosition, MaxMoveSeconds: 5})
	})
	add(func() error {
		return g.AddSignal(&model.Signal{ID: "sigA", Code: "SIG-A", EntryNodeID: "nJ1", GuardSectionID: "s1"})
	})
	return g
}

// divergingRouteToB is a single-point diverging route sigA -> trackB.
func divergingRouteToB() *model.Route {
	return &model.Route{
		ID:                "rtB",
		Code:              "RT-B",
		OriginSignalID:    "sigA",
		TerminalSectionID: "trackB",
		TerminalKind:      model.KindTrack,
		State:             model.RouteLocked,
		PathSections:      []string{"s1", "s2r", "trackB"},
		PointsRequired:   []model.PointRequirement{{PointID: "pt1", Direction: model.DirReverse}},
	}
}

// TestCanClearDivergingShowsYellow guards the bug where a diverging route was
// lit GREEN after point detection. With the point detected REVERSE the
// diverging route must clear to YELLOW (single point), never GREEN.
func TestCanClearDivergingShowsYellow(t *testing.T) {
	g := buildDivergingYard(t)
	c := New()
	r := divergingRouteToB()

	asp, ok := c.CanClear(g, r, r.ID)
	if !ok {
		t.Fatalf("diverging route should be clearable with point detected reverse; got ok=false")
	}
	if asp != model.AspectYellow {
		t.Fatalf("diverging route aspect = %s, want YELLOW (not GREEN)", asp)
	}
}

// TestCanClearStraightShowsGreen confirms the straight route still clears to
// GREEN, so the fix doesn't over-correct the straight case.
func TestCanClearStraightShowsGreen(t *testing.T) {
	g := buildDivergingYard(t)
	// flip the point to NORMAL for the straight route to trackA.
	pt, _ := g.Point("pt1")
	pt.Direction = model.DirNormal
	c := New()
	r := &model.Route{
		ID:                "rtA",
		Code:              "RT-A",
		OriginSignalID:    "sigA",
		TerminalSectionID: "trackA",
		TerminalKind:      model.KindTrack,
		State:             model.RouteLocked,
		PathSections:      []string{"s1", "s2n", "trackA"},
		PointsRequired:   []model.PointRequirement{{PointID: "pt1", Direction: model.DirNormal}},
	}
	asp, ok := c.CanClear(g, r, r.ID)
	if !ok {
		t.Fatalf("straight route should be clearable; got ok=false")
	}
	if asp != model.AspectGreen {
		t.Fatalf("straight route aspect = %s, want GREEN", asp)
	}
}

// TestEnforceKeepsDivergingYellow verifies the fail-safe re-evaluation path
// (used after point detection) keeps a healthy diverging route at YELLOW and
// does not flip it to GREEN.
func TestEnforceKeepsDivergingYellow(t *testing.T) {
	g := buildDivergingYard(t)
	c := New()
	r := divergingRouteToB()

	if asp := c.Enforce(g, r, r.ID); asp != model.AspectYellow {
		t.Fatalf("Enforce diverging route = %s, want YELLOW", asp)
	}
}

// TestAspectForDoubleYellow verifies a diverging route over >1 reverse point
// is assigned DOUBLE_YELLOW.
func TestAspectForDoubleYellow(t *testing.T) {
	c := New()
	r := &model.Route{
		PointsRequired: []model.PointRequirement{
			{PointID: "p1", Direction: model.DirReverse},
			{PointID: "p2", Direction: model.DirReverse},
		},
	}
	if asp := c.AspectFor(r); asp != model.AspectDoubleYellow {
		t.Fatalf("two-point diverging aspect = %s, want DOUBLE_YELLOW", asp)
	}
	// single reverse point -> YELLOW
	r2 := &model.Route{
		PointsRequired: []model.PointRequirement{{PointID: "p1", Direction: model.DirReverse}},
	}
	if asp := c.AspectFor(r2); asp != model.AspectYellow {
		t.Fatalf("single-point diverging aspect = %s, want YELLOW", asp)
	}
	// straight -> GREEN
	r3 := &model.Route{
		PointsRequired: []model.PointRequirement{{PointID: "p1", Direction: model.DirNormal}},
	}
	if asp := c.AspectFor(r3); asp != model.AspectGreen {
		t.Fatalf("straight aspect = %s, want GREEN", asp)
	}
}
