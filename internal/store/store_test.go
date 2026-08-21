package store

import (
	"context"
	"database/sql"
	"testing"

	"task139-railsignal/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestNodeRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	n := &model.Node{ID: "n1", Code: "N1"}
	if err := st.InsertNode(ctx, n); err != nil {
		t.Fatalf("insert: %v", err)
	}
	nodes, err := st.ListNodes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "n1" || nodes[0].Code != "N1" {
		t.Fatalf("round trip = %+v", nodes)
	}
}

func TestSectionRoundTripAndUpdate(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	sec := &model.TrackSection{ID: "s1", Code: "S1", LengthM: 100, Kind: model.KindTrack, FromNodeID: "a", ToNodeID: "b", OccupancyCnt: 0}
	if err := st.InsertSection(ctx, sec); err != nil {
		t.Fatalf("insert: %v", err)
	}
	sec.OccupancyCnt = 2
	sec.LockedByRoute = "rt1"
	if err := st.UpdateSection(ctx, sec); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.GetSection(ctx, "s1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OccupancyCnt != 2 || got.LockedByRoute != "rt1" {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestPointRoundTripWithProtectSections(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	p := &model.Point{
		ID: "pt1", Code: "PT1", NodeID: "n1", HeelSectionID: "h", NormalSectionID: "nrm", ReverseSectionID: "rev",
		Direction: model.DirNormal, Status: model.PointInPosition, MaxMoveSeconds: 5,
		ProtectSections: []string{"protA", "protB"},
	}
	if err := st.InsertPoint(ctx, p); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// mutate runtime fields
	p.Status = model.PointMoving
	p.TargetDirection = model.DirReverse
	p.LockedByRoute = "rt1"
	p.Bypassed = true
	if err := st.UpdatePoint(ctx, p); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.GetPoint(ctx, "pt1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.PointMoving || got.TargetDirection != model.DirReverse || got.LockedByRoute != "rt1" || !got.Bypassed {
		t.Fatalf("runtime fields = %+v", got)
	}
	if len(got.ProtectSections) != 2 || got.ProtectSections[0] != "protA" || got.ProtectSections[1] != "protB" {
		t.Fatalf("protect sections = %+v", got.ProtectSections)
	}
}

func TestSignalRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	sig := &model.Signal{ID: "sig1", Code: "SIG1", EntryNodeID: "n1", GuardSectionID: "s1", Aspect: model.AspectRed, Status: model.SignalSetRed}
	if err := st.InsertSignal(ctx, sig); err != nil {
		t.Fatalf("insert: %v", err)
	}
	sig.Aspect = model.AspectGreen
	sig.Status = model.SignalClearable
	sig.RouteID = "rt1"
	if err := st.UpdateSignal(ctx, sig); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.GetSignal(ctx, "sig1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Aspect != model.AspectGreen || got.Status != model.SignalClearable || got.RouteID != "rt1" {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestRouteRoundTripWithJSON(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := &model.Route{
		ID: "rt1", Code: "RT", OriginSignalID: "sig1", TerminalSectionID: "trackA",
		TerminalKind: model.KindTrack, State: model.RouteLocked,
		PathSections:     []string{"s1", "s2", "trackA"},
		PointsRequired:   []model.PointRequirement{{PointID: "pt1", Direction: model.DirNormal}},
		FlankProtection:  []model.PointRequirement{{PointID: "pt2", Direction: model.DirReverse}},
		ApproachSectionID: "sAP",
		OpenedAt:          100, ReleasedCount: 0,
		ConflictDetail:    []model.ConflictItem{{Kind: "X", RefID: "r", Detail: "d"}},
	}
	if err := st.InsertRoute(ctx, r); err != nil {
		t.Fatalf("insert: %v", err)
	}
	r.State = model.RouteReleasing
	r.ReleasedCount = 2
	if err := st.UpdateRoute(ctx, r); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.GetRoute(ctx, "rt1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != model.RouteReleasing || got.ReleasedCount != 2 {
		t.Fatalf("state/count = %s/%d", got.State, got.ReleasedCount)
	}
	if len(got.PathSections) != 3 || got.PathSections[2] != "trackA" {
		t.Fatalf("path = %+v", got.PathSections)
	}
	if len(got.PointsRequired) != 1 || got.PointsRequired[0].Direction != model.DirNormal {
		t.Fatalf("points = %+v", got.PointsRequired)
	}
	if len(got.FlankProtection) != 1 || got.FlankProtection[0].Direction != model.DirReverse {
		t.Fatalf("flank = %+v", got.FlankProtection)
	}
	if len(got.ConflictDetail) != 1 || got.ConflictDetail[0].Kind != "X" {
		t.Fatalf("conflict = %+v", got.ConflictDetail)
	}
}

func TestEventAppendAndSeq(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		seq, err := st.NextEventSeq(ctx)
		if err != nil {
			t.Fatalf("seq: %v", err)
		}
		if seq != int64(i+1) {
			t.Fatalf("seq = %d, want %d", seq, i+1)
		}
		if _, err := st.AppendEvent(ctx, &model.Event{Seq: seq, Kind: model.EventClockAdvance, Payload: "{}", Clock: i}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	events, err := st.ListEvents(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 || events[0].Seq != 1 || events[2].Seq != 3 {
		t.Fatalf("events = %+v", events)
	}
}

func TestMetaGetSet(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if v, _ := st.GetMeta(ctx, "missing"); v != "" {
		t.Fatalf("missing key should be empty, got %q", v)
	}
	if err := st.SetMeta(ctx, "clock", "42"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, _ := st.GetMeta(ctx, "clock"); v != "42" {
		t.Fatalf("clock = %q, want 42", v)
	}
}

func TestWithTxRollsBackOnErr(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// Insert a node inside a tx that then returns an error -> the node must NOT persist.
	boom := st.WithTx(ctx, func(tx *sql.Tx) error {
		if err := st.InsertNodeTx(ctx, tx, &model.Node{ID: "rb", Code: "RB"}); err != nil {
			return err
		}
		return errInjected
	})
	if boom != errInjected {
		t.Fatalf("tx should surface the injected error, got %v", boom)
	}
	nodes, err := st.ListNodes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, n := range nodes {
		if n.ID == "rb" {
			t.Fatalf("rolled-back node should not be present")
		}
	}
}

// errInjected is a sentinel returned by a tx closure to verify rollback.
var errInjected = newSentinel("injected rollback")

type sentinelErr struct{ msg string }

func (s *sentinelErr) Error() string { return s.msg }
func newSentinel(msg string) error    { return &sentinelErr{msg: msg} }
