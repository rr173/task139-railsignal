package signal

import (
	"testing"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/topology"
)

// buildYard constructs the simple facing-point yard used across the engine's
// tests so the signal unit tests can drive CanClear directly.
//
//	AP --J1-- s1 --J2(pt)--> normal: s2n --> A --> trackA(terminal)
//	                      \-> reverse: s2r --> B --> trackB(terminal)
//
// sigA at J1 guards s1, which is PathSections[0] of a sigA->trackA route.
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

// route returns a sigA->trackA route (point normal, straight). Its
// PathSections[0] is s1 — the signal's guard section, i.e. the first protected
// section.
func straightRoute() *model.Route {
	return &model.Route{
		ID:               "rt1",
		OriginSignalID:   "sigA",
		TerminalSectionID: "trackA",
		State:            model.RouteLocked,
		PathSections:     []string{"s1", "s2n", "trackA"},
		PointsRequired:   []model.PointRequirement{{PointID: "pt1", Direction: model.DirNormal}},
	}
}

// TestCanClearStraightRoute verifies the happy path: an all-free straight
// route clears GREEN.
func TestCanClearStraightRoute(t *testing.T) {
	g := buildYard(t)
	c := New()
	asp, ok := c.CanClear(g, straightRoute(), "rt1")
	if !ok || asp != model.AspectGreen {
		t.Fatalf("CanClear = (%s, %v), want (GREEN, true)", asp, ok)
	}
}

// TestCanClearFirstProtectedSectionOccupied verifies the fail-safe rule at the
// unit level: once the first protected section (PathSections[0], the guard
// section s1) is occupied, the signal must NOT be clearable — CanClear must
// check index 0, not skip it.
func TestCanClearFirstProtectedSectionOccupied(t *testing.T) {
	g := buildYard(t)
	c := New()
	// occupy s1 = PathSections[0], the signal's own guard section.
	if s, ok := g.Section("s1"); ok {
		s.OccupancyCnt = 1
	}
	asp, ok := c.CanClear(g, straightRoute(), "rt1")
	if ok {
		t.Fatalf("CanClear = (%s, %v), want RED/false when first protected section occupied", asp, ok)
	}
	if asp != model.AspectRed {
		t.Fatalf("aspect = %s, want RED", asp)
	}
}

// TestEnforceFirstProtectedSectionOccupied verifies Enforce (the fail-safe
// re-evaluator used on clock advance / re-open paths) clamps to RED when the
// first protected section is occupied.
func TestEnforceFirstProtectedSectionOccupied(t *testing.T) {
	g := buildYard(t)
	c := New()
	if s, ok := g.Section("s1"); ok {
		s.OccupancyCnt = 1
	}
	if asp := c.Enforce(g, straightRoute(), "rt1"); asp != model.AspectRed {
		t.Fatalf("Enforce = %s, want RED when first protected section occupied", asp)
	}
}
