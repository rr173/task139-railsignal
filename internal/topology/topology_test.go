package topology

import (
	"testing"

	"task139-railsignal/internal/model"
)

// mustAdd is a test helper that adds an entity or fails the test.
func mustAdd(t *testing.T, g *Graph, add func() error) {
	t.Helper()
	if err := add(); err != nil {
		t.Fatalf("add: %v", err)
	}
}

// buildSimpleYard constructs a yard with a facing point for tests:
//
//	AP --J1-- s1 --J2(pt)--> normal: s2n --> A --> trackA(terminal)
//	                      \-> reverse: s2r --> B --> trackB(terminal)
//
// and returns the created entity ids for assertions.
func buildSimpleYard(t *testing.T) *Graph {
	g := New()
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nAP", Code: "N-AP"}) })
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nJ1", Code: "N-J1"}) })
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nJ2", Code: "N-J2"}) })
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nA", Code: "N-A"}) })
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nB", Code: "N-B"}) })
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nT1", Code: "N-T1"}) })
	mustAdd(t, g, func() error { return g.AddNode(&model.Node{ID: "nT2", Code: "N-T2"}) })

	mustAdd(t, g, func() error {
		return g.AddSection(&model.TrackSection{ID: "sAP", Code: "SEC-AP", LengthM: 100, Kind: model.KindBlock, FromNodeID: "nAP", ToNodeID: "nJ1"})
	})
	mustAdd(t, g, func() error {
		return g.AddSection(&model.TrackSection{ID: "s1", Code: "SEC-S1", LengthM: 80, Kind: model.KindLadder, FromNodeID: "nJ1", ToNodeID: "nJ2"})
	})
	mustAdd(t, g, func() error {
		return g.AddSection(&model.TrackSection{ID: "s2n", Code: "SEC-S2N", LengthM: 60, Kind: model.KindLadder, FromNodeID: "nJ2", ToNodeID: "nA"})
	})
	mustAdd(t, g, func() error {
		return g.AddSection(&model.TrackSection{ID: "s2r", Code: "SEC-S2R", LengthM: 60, Kind: model.KindLadder, FromNodeID: "nJ2", ToNodeID: "nB"})
	})
	mustAdd(t, g, func() error {
		return g.AddSection(&model.TrackSection{ID: "trackA", Code: "SEC-TRACKA", LengthM: 200, Kind: model.KindTrack, FromNodeID: "nA", ToNodeID: "nT1"})
	})
	mustAdd(t, g, func() error {
		return g.AddSection(&model.TrackSection{ID: "trackB", Code: "SEC-TRACKB", LengthM: 200, Kind: model.KindTrack, FromNodeID: "nB", ToNodeID: "nT2"})
	})
	mustAdd(t, g, func() error {
		return g.AddPoint(&model.Point{ID: "pt1", Code: "PT1", NodeID: "nJ2", HeelSectionID: "s1", NormalSectionID: "s2n", ReverseSectionID: "s2r", Direction: model.DirNormal, MaxMoveSeconds: 5})
	})
	mustAdd(t, g, func() error {
		return g.AddSignal(&model.Signal{ID: "sigA", Code: "SIG-A", EntryNodeID: "nJ1", GuardSectionID: "s1"})
	})
	return g
}

func TestAddNodeRejectsDuplicate(t *testing.T) {
	g := New()
	if err := g.AddNode(&model.Node{ID: "n1", Code: "N1"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := g.AddNode(&model.Node{ID: "n1", Code: "N1"}); err == nil {
		t.Fatal("duplicate node should be rejected")
	}
	if err := g.AddNode(&model.Node{ID: "", Code: "X"}); err == nil {
		t.Fatal("empty id should be rejected")
	}
}

func TestAddSectionRejectsCycle(t *testing.T) {
	g := buildSimpleYard(t)
	// adding an edge nT1 -> nAP closes a loop nAP->J1->J2->A->T1->AP
	err := g.AddSection(&model.TrackSection{ID: "sLoop", Code: "SEC-LOOP", LengthM: 10, Kind: model.KindLadder, FromNodeID: "nT1", ToNodeID: "nAP"})
	if err == nil {
		t.Fatal("cycle-forming section should be rejected")
	}
}

func TestAddSectionRejectsSelfLoop(t *testing.T) {
	g := buildSimpleYard(t)
	err := g.AddSection(&model.TrackSection{ID: "sSelf", Code: "S", LengthM: 10, Kind: model.KindLadder, FromNodeID: "nJ1", ToNodeID: "nJ1"})
	if err == nil {
		t.Fatal("self-loop should be rejected")
	}
}

func TestAddPointRejectsRepeatedLegs(t *testing.T) {
	g := New()
	_ = g.AddNode(&model.Node{ID: "nX", Code: "X"})
	_ = g.AddNode(&model.Node{ID: "nY", Code: "Y"})
	_ = g.AddSection(&model.TrackSection{ID: "a", Code: "A", LengthM: 10, Kind: model.KindTrack, FromNodeID: "nX", ToNodeID: "nY"})
	err := g.AddPoint(&model.Point{ID: "p", Code: "P", NodeID: "nX", HeelSectionID: "a", NormalSectionID: "a", ReverseSectionID: "a", Direction: model.DirNormal, MaxMoveSeconds: 5})
	if err == nil {
		t.Fatal("point with repeated leg sections should be rejected")
	}
}

func TestExpandStraightRoute(t *testing.T) {
	g := buildSimpleYard(t)
	r, err := g.Expand("sigA", "trackA")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	// path: s1, s2n, trackA (approach = sAP)
	want := []string{"s1", "s2n", "trackA"}
	if len(r.PathSections) != len(want) {
		t.Fatalf("path len = %d, want %d (%v)", len(r.PathSections), len(want), r.PathSections)
	}
	for i, sid := range want {
		if r.PathSections[i] != sid {
			t.Fatalf("path[%d] = %s, want %s", i, r.PathSections[i], sid)
		}
	}
	if r.ApproachSectionID != "sAP" {
		t.Fatalf("approach = %s, want sAP", r.ApproachSectionID)
	}
	// point pt1 required to normal
	if len(r.PointsRequired) != 1 || r.PointsRequired[0].PointID != "pt1" || r.PointsRequired[0].Direction != model.DirNormal {
		t.Fatalf("points required = %+v, want pt1=N", r.PointsRequired)
	}
	if r.Diverging {
		t.Fatal("straight route should not be diverging")
	}
}

func TestExpandReverseRoute(t *testing.T) {
	g := buildSimpleYard(t)
	// move the point to reverse first so expansion picks the reverse leg.
	p, _ := g.Point("pt1")
	p.Direction = model.DirReverse
	p.Status = model.PointInPosition
	r, err := g.Expand("sigA", "trackB")
	if err != nil {
		t.Fatalf("expand reverse: %v", err)
	}
	want := []string{"s1", "s2r", "trackB"}
	if len(r.PathSections) != len(want) {
		t.Fatalf("path len = %d, want %d (%v)", len(r.PathSections), len(want), r.PathSections)
	}
	for i, sid := range want {
		if r.PathSections[i] != sid {
			t.Fatalf("path[%d] = %s, want %s", i, r.PathSections[i], sid)
		}
	}
	if len(r.PointsRequired) != 1 || r.PointsRequired[0].Direction != model.DirReverse {
		t.Fatalf("points required = %+v, want pt1=R", r.PointsRequired)
	}
	if !r.Diverging {
		t.Fatal("reverse route should be diverging")
	}
}

func TestExpandDeadEndRejected(t *testing.T) {
	g := buildSimpleYard(t)
	// A truly isolated terminal: two brand-new nodes that no existing section
	// touches, so there is no path from sigA.
	_ = g.AddNode(&model.Node{ID: "nIso1", Code: "N-ISO1"})
	_ = g.AddNode(&model.Node{ID: "nIso2", Code: "N-ISO2"})
	_ = g.AddSection(&model.TrackSection{ID: "sIso", Code: "ISO", LengthM: 10, Kind: model.KindTrack, FromNodeID: "nIso1", ToNodeID: "nIso2"})
	if _, err := g.Expand("sigA", "sIso"); err == nil {
		t.Fatal("expansion to an isolated section should fail")
	}
}

func TestRemoveSectionRejectsIfPointReferences(t *testing.T) {
	g := buildSimpleYard(t)
	if err := g.RemoveSection("s1"); err == nil {
		t.Fatal("removing a section referenced by a point should be rejected")
	}
}
