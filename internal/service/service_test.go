package service

import (
	"context"
	"testing"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/store"
)

func newServiceWithYard(t *testing.T) *Service {
	t.Helper()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)
	ctx := context.Background()
	// reset to a clean state (New may carry over nothing for a fresh store).
	_ = svc.ResetLayout(ctx)
	// build the simple yard used across tests, via the service layer.
	nodes := []string{"N-AP", "N-J1", "N-J2", "N-A", "N-B", "N-T1", "N-T2"}
	nodeIDs := map[string]string{}
	for _, code := range nodes {
		n, err := svc.AddNode(ctx, AddNodeRequest{Code: code})
		if err != nil {
			t.Fatalf("add node %s: %v", code, err)
		}
		nodeIDs[code] = n.ID
	}
	sections := []struct {
		code string
		kind model.TrackSectionKind
		from string
		to   string
	}{
		{"SEC-AP", model.KindBlock, "N-AP", "N-J1"},
		{"SEC-S1", model.KindLadder, "N-J1", "N-J2"},
		{"SEC-S2N", model.KindLadder, "N-J2", "N-A"},
		{"SEC-S2R", model.KindLadder, "N-J2", "N-B"},
		{"SEC-TRACKA", model.KindTrack, "N-A", "N-T1"},
		{"SEC-TRACKB", model.KindTrack, "N-B", "N-T2"},
	}
	secIDs := map[string]string{}
	for _, s := range sections {
		sec, err := svc.AddSection(ctx, AddSectionRequest{Code: s.code, LengthM: 100, Kind: s.kind, FromNodeID: nodeIDs[s.from], ToNodeID: nodeIDs[s.to]})
		if err != nil {
			t.Fatalf("add section %s: %v", s.code, err)
		}
		secIDs[s.code] = sec.ID
	}
	// point at N-J2
	pt, err := svc.AddPoint(ctx, AddPointRequest{
		Code: "PT1", NodeID: nodeIDs["N-J2"],
		HeelSectionID: secIDs["SEC-S1"], NormalSectionID: secIDs["SEC-S2N"], ReverseSectionID: secIDs["SEC-S2R"],
		Direction: model.DirNormal, MaxMoveSeconds: 5,
	})
	if err != nil {
		t.Fatalf("add point: %v", err)
	}
	_ = pt
	// signal at N-J1 guarding SEC-S1
	sig, err := svc.AddSignal(ctx, AddSignalRequest{Code: "SIG-A", EntryNodeID: nodeIDs["N-J1"], GuardSectionID: secIDs["SEC-S1"]})
	if err != nil {
		t.Fatalf("add signal: %v", err)
	}
	// stash ids on the test via the service's cache: retrieve them.
	t.Logf("yard built: signal=%s point=%s", sig.ID, pt.ID)
	return svc
}

func TestRequestStraightRouteClearsImmediately(t *testing.T) {
	svc := newServiceWithYard(t)
	ctx := context.Background()
	layout := svc.Layout(ctx)
	var sigA, trackA string
	for _, s := range layout.Signals {
		if s.Code == "SIG-A" {
			sigA = s.ID
		}
	}
	for _, s := range layout.Sections {
		if s.Code == "SEC-TRACKA" {
			trackA = s.ID
		}
	}
	res, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sigA, TerminalSectionID: trackA})
	if err != nil {
		t.Fatalf("request route: %v", err)
	}
	if !res.Cleared || res.Aspect != model.AspectGreen {
		t.Fatalf("straight route should clear GREEN immediately; cleared=%v aspect=%s", res.Cleared, res.Aspect)
	}
	if res.Route.State != model.RouteLocked {
		t.Fatalf("route state = %s, want LOCKED", res.Route.State)
	}
}

func TestDuplicateOriginRefused(t *testing.T) {
	svc := newServiceWithYard(t)
	ctx := context.Background()
	layout := svc.Layout(ctx)
	var sigA, trackA string
	for _, s := range layout.Signals {
		if s.Code == "SIG-A" {
			sigA = s.ID
		}
	}
	for _, s := range layout.Sections {
		if s.Code == "SEC-TRACKA" {
			trackA = s.ID
		}
	}
	if _, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sigA, TerminalSectionID: trackA}); err != nil {
		t.Fatalf("first route: %v", err)
	}
	// second request from the same origin must be refused.
	_, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sigA, TerminalSectionID: trackA})
	if err == nil {
		t.Fatal("duplicate origin route should be refused")
	}
}

func TestOccupancyAdvancesRouteAndCancelBlocksRelease(t *testing.T) {
	svc := newServiceWithYard(t)
	ctx := context.Background()
	layout := svc.Layout(ctx)
	var sigA, trackA, s1, s2n, approach string
	for _, s := range layout.Signals {
		if s.Code == "SIG-A" {
			sigA = s.ID
		}
	}
	for _, sec := range layout.Sections {
		switch sec.Code {
		case "SEC-TRACKA":
			trackA = sec.ID
		case "SEC-S1":
			s1 = sec.ID
		case "SEC-S2N":
			s2n = sec.ID
		case "SEC-AP":
			approach = sec.ID
		}
	}
	res, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sigA, TerminalSectionID: trackA})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	rtID := res.Route.ID
	// occupy approach -> TRAIN_COMING
	if _, err := svc.ReportOccupancy(ctx, OccupancyEvent{SectionID: approach}); err != nil {
		t.Fatalf("occ approach: %v", err)
	}
	// occupy s1 -> TRAIN_IN_ROUTE
	if _, err := svc.ReportOccupancy(ctx, OccupancyEvent{SectionID: s1}); err != nil {
		t.Fatalf("occ s1: %v", err)
	}
	rt, _ := svc.GetRoute(ctx, rtID)
	if rt.State != model.RouteTrainInRoute {
		t.Fatalf("state = %s, want TRAIN_IN_ROUTE", rt.State)
	}
	// cancel occupied -> CANCEL_PENDING (rear-of-train protection).
	rt, err = svc.CancelRoute(ctx, rtID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if rt.State != model.RouteCancelPending {
		t.Fatalf("state = %s, want CANCEL_PENDING", rt.State)
	}
	// drive train forward and clear in order -> CANCELLED.
	_, _ = svc.ReportOccupancy(ctx, OccupancyEvent{SectionID: s2n})
	_, _ = svc.ReportOccupancy(ctx, OccupancyEvent{SectionID: trackA})
	_, _ = svc.ReportClearance(ctx, ClearanceEvent{SectionID: s1})
	_, _ = svc.ReportClearance(ctx, ClearanceEvent{SectionID: s2n})
	_, _ = svc.ReportClearance(ctx, ClearanceEvent{SectionID: approach})
	_, _ = svc.ReportClearance(ctx, ClearanceEvent{SectionID: trackA})
	rt, _ = svc.GetRoute(ctx, rtID)
	if rt.State != model.RouteCancelled {
		t.Fatalf("after full clearance state = %s, want CANCELLED", rt.State)
	}
}

func TestReconcileRestoresState(t *testing.T) {
	svc := newServiceWithYard(t)
	ctx := context.Background()
	layout := svc.Layout(ctx)
	var sigA, trackA string
	for _, s := range layout.Signals {
		if s.Code == "SIG-A" {
			sigA = s.ID
		}
	}
	for _, s := range layout.Sections {
		if s.Code == "SEC-TRACKA" {
			trackA = s.ID
		}
	}
	if _, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sigA, TerminalSectionID: trackA}); err != nil {
		t.Fatalf("route: %v", err)
	}
	before := svc.InterlockingReport(ctx)
	// force a reload/reconcile (simulated restart).
	rep, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rep.Routes != len(before.Routes) {
		t.Fatalf("route count changed across reconcile: before=%d after=%d", len(before.Routes), rep.Routes)
	}
	after := svc.InterlockingReport(ctx)
	// the locked route should still be LOCKED.
	lockedCount := 0
	for _, r := range after.Routes {
		if r.State == model.RouteLocked {
			lockedCount++
		}
	}
	if lockedCount != 1 {
		t.Fatalf("locked route count after reconcile = %d, want 1", lockedCount)
	}
}

func TestBypassRefusedWhenHealthy(t *testing.T) {
	svc := newServiceWithYard(t)
	ctx := context.Background()
	layout := svc.Layout(ctx)
	var pt1 string
	for _, p := range layout.Points {
		if p.Code == "PT1" {
			pt1 = p.ID
		}
	}
	// healthy point cannot be bypassed.
	if _, err := svc.BypassPoint(ctx, pt1, true); err == nil {
		t.Fatal("bypassing a healthy point should be refused")
	}
}

// TestRouteRefusedWhenTerminalOccupied guards the "终端股道已经被占用时，
// 调度请求不得建立并开放进入该股道的进路" invariant: when the destination
// track is occupied (压车), a route request must not establish or open a route
// into it — the interlocking check reports TERMINAL_OCCUPIED and the signal
// stays at RED.
func TestRouteRefusedWhenTerminalOccupied(t *testing.T) {
	svc := newServiceWithYard(t)
	ctx := context.Background()
	layout := svc.Layout(ctx)
	var sigA, trackA string
	for _, s := range layout.Signals {
		if s.Code == "SIG-A" {
			sigA = s.ID
		}
	}
	for _, sec := range layout.Sections {
		if sec.Code == "SEC-TRACKA" {
			trackA = sec.ID
		}
	}
	// occupy the terminal track before dispatch.
	if _, err := svc.ReportOccupancy(ctx, OccupancyEvent{SectionID: trackA}); err != nil {
		t.Fatalf("occupy terminal: %v", err)
	}
	res, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sigA, TerminalSectionID: trackA})
	if err != nil {
		t.Fatalf("request route: %v (conflict should be reported, not an error)", err)
	}
	if res.Cleared {
		t.Fatalf("route into occupied terminal must NOT clear; cleared=true")
	}
	if res.Aspect != model.AspectRed {
		t.Fatalf("signal must stay RED into occupied terminal; aspect=%s", res.Aspect)
	}
	if res.Route.State != model.RouteConflict {
		t.Fatalf("route state = %s, want CONFLICT", res.Route.State)
	}
	found := false
	for _, it := range res.Route.ConflictDetail {
		if it.Kind == "TERMINAL_OCCUPIED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TERMINAL_OCCUPIED in conflict detail, got %+v", res.Route.ConflictDetail)
	}
	// the origin signal must remain uncleared.
	sig, _ := svc.GetSignal(ctx, sigA)
	if sig.RouteID != "" {
		t.Fatalf("signal must not be cleared for a route; routeID=%q", sig.RouteID)
	}
}