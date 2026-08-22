package point

import (
	"testing"

	"task139-railsignal/internal/model"
)

func newPoint(id string, dir model.PointDirection, protect ...string) *model.Point {
	return &model.Point{
		ID:             id,
		Code:           id,
		NodeID:         "n",
		HeelSectionID:  "heel",
		NormalSectionID: "normal",
		ReverseSectionID: "reverse",
		Direction:      dir,
		Status:         model.PointInPosition,
		MaxMoveSeconds: 5,
		ProtectSections: protect,
	}
}

func occupiedFn(set ...string) func(string) bool {
	m := map[string]bool{}
	for _, s := range set {
		m[s] = true
	}
	return func(sid string) bool { return m[sid] }
}

func TestCanMoveAlreadyAtTarget(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal)
	ok, err := m.CanMove(p, model.DirNormal, occupiedFn())
	if ok || err != nil {
		t.Fatalf("already-at-target should be ok=false err=nil, got ok=%v err=%v", ok, err)
	}
}

func TestIssueMoveAndDetect(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal)
	if ok, err := m.CanMove(p, model.DirReverse, occupiedFn()); !ok || err != nil {
		t.Fatalf("can move to reverse: ok=%v err=%v", ok, err)
	}
	m.IssueMove(p, model.DirReverse, 100)
	if p.Status != model.PointMoving || p.TargetDirection != model.DirReverse {
		t.Fatalf("after issue: status=%s target=%s", p.Status, p.TargetDirection)
	}
	if p.MoveDeadline != 105 {
		t.Fatalf("deadline = %d, want 105", p.MoveDeadline)
	}
	if m.Detect(p, model.DirNormal) {
		t.Fatal("detecting the non-target direction during a move must be ignored (no transition)")
	}
	if p.Status != model.PointMoving {
		t.Fatalf("status should still be MOVING after ignored detection, got %s", p.Status)
	}
	if !m.Detect(p, model.DirReverse) {
		t.Fatal("detecting target should transition")
	}
	if p.Status != model.PointInPosition || p.Direction != model.DirReverse {
		t.Fatalf("after detect: status=%s dir=%s", p.Status, p.Direction)
	}
}

func TestTickTimeout(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal)
	m.IssueMove(p, model.DirReverse, 100)
	// deadline = 105
	if m.TickTimeout(p, 104) {
		t.Fatal("should not time out before deadline")
	}
	if !m.TickTimeout(p, 106) {
		t.Fatal("should time out at/after deadline")
	}
	if p.Status != model.PointOutOfCorrespondence {
		t.Fatalf("status = %s, want OUT_OF_CORRESPONDENCE", p.Status)
	}
	// late detection recovers it.
	if !m.Detect(p, model.DirReverse) {
		t.Fatal("late detection matching target should recover")
	}
	if p.Status != model.PointInPosition {
		t.Fatalf("after late detect: status=%s", p.Status)
	}
}

func TestCanMoveRejectsFaultedAndOutOfCorrespondence(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal)
	m.ForceFault(p)
	if _, err := m.CanMove(p, model.DirReverse, occupiedFn()); err == nil {
		t.Fatal("faulted point should not be movable")
	}
	p2 := newPoint("p2", model.DirNormal)
	p2.Status = model.PointOutOfCorrespondence
	p2.TargetDirection = model.DirReverse
	if _, err := m.CanMove(p2, model.DirReverse, occupiedFn()); err == nil {
		t.Fatal("OUT_OF_CORRESPONDENCE (not bypassed) should not be movable")
	}
}

func TestAntiSqueezeBlocksMove(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal, "protectSec")
	// protect section occupied -> cannot move.
	if _, err := m.CanMove(p, model.DirReverse, occupiedFn("protectSec")); err == nil {
		t.Fatal("moving a point under an occupied protect section must be refused")
	}
	// protect section free -> can move.
	if ok, err := m.CanMove(p, model.DirReverse, occupiedFn()); !ok || err != nil {
		t.Fatalf("should be movable when protect free, got ok=%v err=%v", ok, err)
	}
}

func TestBypassOnlyInDegradedState(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal)
	// healthy point cannot be bypassed.
	if err := m.Bypass(p, true, occupiedFn()); err == nil {
		t.Fatal("bypassing a healthy point should be refused")
	}
	// degraded (out-of-correspondence) point with target reverse.
	p.Status = model.PointOutOfCorrespondence
	p.TargetDirection = model.DirReverse
	if err := m.Bypass(p, true, occupiedFn()); err != nil {
		t.Fatalf("bypass degraded point: %v", err)
	}
	if !p.Bypassed || p.Direction != model.DirReverse || p.Status != model.PointInPosition {
		t.Fatalf("bypass state wrong: bypassed=%v dir=%s status=%s", p.Bypassed, p.Direction, p.Status)
	}
	// bypassing with occupied protect section is refused even when degraded.
	p2 := newPoint("p2", model.DirNormal, "protectSec")
	p2.Status = model.PointOutOfCorrespondence
	p2.TargetDirection = model.DirReverse
	if err := m.Bypass(p2, true, occupiedFn("protectSec")); err == nil {
		t.Fatal("bypass under an occupied protect section must be refused")
	}
}

func TestForceAndClearFault(t *testing.T) {
	m := New()
	p := newPoint("p", model.DirNormal)
	m.ForceFault(p)
	if p.Status != model.PointFault {
		t.Fatalf("status = %s, want FAULT", p.Status)
	}
	m.ClearFault(p)
	if p.Status != model.PointInPosition {
		t.Fatalf("status = %s, want IN_POSITION", p.Status)
	}
}
