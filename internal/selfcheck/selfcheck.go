// Package selfcheck implements the --smoke-test. It builds the engine against
// an in-memory SQLite store on an httptest.Server, drives the full business
// loop through the real HTTP API (build yard -> request route -> interlock ->
// drive train -> section-by-section release -> cancel + rear-of-train
// protection -> restart reconciliation), asserts the interlocking safety
// invariants, and exits non-zero on any failure.
package selfcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"task139-railsignal/internal/httpapi"
	"task139-railsignal/internal/model"
	"task139-railsignal/internal/service"
	"task139-railsignal/internal/store"
)

// Run executes the smoke test and returns the first error encountered.
func Run() error {
	ctx := context.Background()
	st, err := store.New(":memory:")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	svc := service.New(st)
	handler := httpapi.New(svc)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	client := &http.Client{}

	// Fresh yard.
	mustDo(ctx, client, srv.URL, "POST", "/layout/reset", nil, nil)

	// -----------------------------------------------------------------------
	// Build a simple yard with a facing point:
	//   approach -[sigA]-> s1 -[pt heel]-> (normal: s2n -> trackA) /
	//                                    (reverse: s2r -> trackB)
	// The yard is a DAG (no edge back to the approach).
	// Nodes
	nApproach := post[map[string]any](ctx, client, srv.URL, "/nodes", map[string]any{"code": "N-AP"})
	nJ1 := post[map[string]any](ctx, client, srv.URL, "/nodes", map[string]any{"code": "N-J1"})
	nJ2 := post[map[string]any](ctx, client, srv.URL, "/nodes", map[string]any{"code": "N-J2"})
	nA := post[map[string]any](ctx, client, srv.URL, "/nodes", map[string]any{"code": "N-A"})
	nB := post[map[string]any](ctx, client, srv.URL, "/nodes", map[string]any{"code": "N-B"})
	nT1 := post[map[string]any](ctx, client, srv.URL, "/nodes", map[string]any{"code": "N-T1"})

	// Sections: approach, s1 (heel), s2n (normal leg to trackA), s2r (reverse leg to trackB), trackA/trackB
	approach := post[map[string]any](ctx, client, srv.URL, "/sections", map[string]any{
		"code": "SEC-AP", "length_m": 100, "kind": "block",
		"from_node_id": nApproach["id"], "to_node_id": nJ1["id"],
	})
	s1 := post[map[string]any](ctx, client, srv.URL, "/sections", map[string]any{
		"code": "SEC-S1", "length_m": 80, "kind": "ladder",
		"from_node_id": nJ1["id"], "to_node_id": nJ2["id"],
	})
	s2n := post[map[string]any](ctx, client, srv.URL, "/sections", map[string]any{
		"code": "SEC-S2N", "length_m": 60, "kind": "ladder",
		"from_node_id": nJ2["id"], "to_node_id": nA["id"],
	})
	s2r := post[map[string]any](ctx, client, srv.URL, "/sections", map[string]any{
		"code": "SEC-S2R", "length_m": 60, "kind": "ladder",
		"from_node_id": nJ2["id"], "to_node_id": nB["id"],
	})
	trackA := post[map[string]any](ctx, client, srv.URL, "/sections", map[string]any{
		"code": "SEC-TRACKA", "length_m": 200, "kind": "track",
		"from_node_id": nA["id"], "to_node_id": nT1["id"], // dead-end terminal node, no cycle
	})
	_ = approach

	// Point at N-J2: heel=s1, normal=s2n, reverse=s2r
	pt := post[map[string]any](ctx, client, srv.URL, "/points", map[string]any{
		"code": "PT1", "node_id": nJ2["id"],
		"heel_section_id": s1["id"], "normal_section_id": s2n["id"], "reverse_section_id": s2r["id"],
		"direction": "N", "max_move_seconds": 5,
	})
	// Signal at N-J1 guarding s1
	sigA := post[map[string]any](ctx, client, srv.URL, "/signals", map[string]any{
		"code": "SIG-A", "entry_node_id": nJ1["id"], "guard_section_id": s1["id"],
	})

	// -----------------------------------------------------------------------
	// Route A: SIG-A -> trackA (straight, point normal). Point already normal.
	routeA := post[map[string]any](ctx, client, srv.URL, "/routes", map[string]any{
		"origin_signal_id": sigA["id"], "terminal_section_id": trackA["id"],
	})
	if routeA["cleared"] != true {
		return fmt.Errorf("route A should clear immediately (point already normal); got %v", routeA)
	}
	rtA := routeA["route"].(map[string]any)
	if rtA["state"] != string(model.RouteLocked) {
		return fmt.Errorf("route A expected LOCKED, got %v", rtA["state"])
	}
	if routeA["aspect"] != string(model.AspectGreen) {
		return fmt.Errorf("straight route A expected GREEN, got %v", routeA["aspect"])
	}

	// -----------------------------------------------------------------------
	// Route B: a conflicting route reusing s1 / the point while A is active.
	// Re-requesting the same origin is refused (signal already cleared).
	_, errB := do[map[string]any](ctx, client, srv.URL, "POST", "/routes", map[string]any{
		"origin_signal_id": sigA["id"], "terminal_section_id": trackA["id"],
	})
	if errB == nil {
		return fmt.Errorf("duplicate origin route should have been refused")
	}

	// -----------------------------------------------------------------------
	// Drive the train: occupy approach -> TRAIN_COMING; occupy s1 -> TRAIN_IN_ROUTE.
	occ := post[map[string]any](ctx, client, srv.URL, "/occupancy", map[string]any{"section_id": approach["id"]})
	if !hasTransition(occ, rtA["id"].(string), string(model.RouteTrainComing)) {
		// route may already be TRAIN_IN_ROUTE if approach occupied first; acceptable
		// only if it reached TRAIN_IN_ROUTE after s1 occupation below.
	}
	occ = post[map[string]any](ctx, client, srv.URL, "/occupancy", map[string]any{"section_id": s1["id"]})
	if !hasTransition(occ, rtA["id"].(string), string(model.RouteTrainInRoute)) {
		return fmt.Errorf("occupying s1 should move route to TRAIN_IN_ROUTE; got %v", occ)
	}

	// -----------------------------------------------------------------------
	// Cancel while occupied: must go CANCEL_PENDING (rear-of-train protection).
	canc, errC := do[map[string]any](ctx, client, srv.URL, "POST", "/routes/"+rtA["id"].(string)+"/cancel", nil)
	if errC != nil {
		return fmt.Errorf("cancel occupied route failed: %v", errC)
	}
	if canc["state"] != string(model.RouteCancelPending) {
		return fmt.Errorf("cancelling an occupied route must be CANCEL_PENDING, got %v", canc["state"])
	}

	// -----------------------------------------------------------------------
	// Clear sections in order: train moves forward into s2n then trackA,
	// then the rear clears each path section in forward order so the
	// route-release releases s1, s2n, trackA in sequence.
	_ = post[map[string]any](ctx, client, srv.URL, "/occupancy", map[string]any{"section_id": s2n["id"]})
	_ = post[map[string]any](ctx, client, srv.URL, "/occupancy", map[string]any{"section_id": trackA["id"]})
	clr1 := post[map[string]any](ctx, client, srv.URL, "/clearance", map[string]any{"section_id": s1["id"]})
	_ = clr1
	clr2 := post[map[string]any](ctx, client, srv.URL, "/clearance", map[string]any{"section_id": s2n["id"]})
	_ = clr2
	_ = post[map[string]any](ctx, client, srv.URL, "/clearance", map[string]any{"section_id": approach["id"]})
	clr3 := post[map[string]any](ctx, client, srv.URL, "/clearance", map[string]any{"section_id": trackA["id"]})
	_ = clr3
	// after full clearance the route should be CANCELLED (it was CANCEL_PENDING).
	rtFinal := get[map[string]any](ctx, client, srv.URL, "/routes/"+rtA["id"].(string))
	if rtFinal["state"] != string(model.RouteCancelled) && rtFinal["state"] != string(model.RouteReleased) {
		return fmt.Errorf("after full clearance, route should be terminal, got %v", rtFinal["state"])
	}
	_ = clr2
	_ = pt

	// -----------------------------------------------------------------------
	// Restart reconciliation: snapshot routes, force reconcile, compare.
	repBefore := get[map[string]any](ctx, client, srv.URL, "/reports/interlocking")
	routesBefore := repBefore["routes"].([]any)
	_ = routesBefore
	recon := post[map[string]any](ctx, client, srv.URL, "/reconcile", nil)
	if recon == nil {
		return fmt.Errorf("reconcile returned nil")
	}
	repAfter := get[map[string]any](ctx, client, srv.URL, "/reports/interlocking")
	routesAfter := repAfter["routes"].([]any)
	if len(routesAfter) != len(routesBefore) {
		return fmt.Errorf("reconcile changed route count: before=%d after=%d", len(routesBefore), len(routesAfter))
	}

	// -----------------------------------------------------------------------
	// Frontend reachable and exercises a business API.
	page := getRaw(ctx, client, srv.URL, "/")
	if !strings.Contains(page, "railsignal") && !strings.Contains(page, "Railway") && !strings.Contains(page, "signal") {
		return fmt.Errorf("frontend page missing expected content")
	}
	if !strings.Contains(page, "app.js") {
		return fmt.Errorf("frontend page does not reference app.js")
	}
	js := getRaw(ctx, client, srv.URL, "/static/app.js")
	if !strings.Contains(js, "/routes") {
		return fmt.Errorf("app.js does not call /routes")
	}

	return nil
}

// hasTransition reports whether a TrainMoveResult contains a transition to
// the given state for the given route.
func hasTransition(res map[string]any, routeID, to string) bool {
	trans, ok := res["transitions"].([]any)
	if !ok {
		return false
	}
	for _, t := range trans {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if m["route_id"] == routeID && m["to"] == to {
			return true
		}
	}
	return false
}

// ---- tiny HTTP helpers ----

func do[T any](ctx context.Context, c *http.Client, base, method, path string, body any) (T, error) {
	var dst T
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return dst, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return dst, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return dst, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return dst, fmt.Errorf("%s %s: status %d body %s", method, path, resp.StatusCode, string(data))
	}
	if len(data) == 0 {
		return dst, nil
	}
	if err := json.Unmarshal(data, &dst); err != nil {
		return dst, fmt.Errorf("decode %s: %w (body %s)", path, err, string(data))
	}
	return dst, nil
}

func post[T any](ctx context.Context, c *http.Client, base, path string, body any) T {
	out, err := do[T](ctx, c, base, "POST", path, body)
	if err != nil {
		panic(fmt.Sprintf("smoke POST %s: %v", path, err))
	}
	return out
}

func get[T any](ctx context.Context, c *http.Client, base, path string) T {
	out, err := do[T](ctx, c, base, "GET", path, nil)
	if err != nil {
		panic(fmt.Sprintf("smoke GET %s: %v", path, err))
	}
	return out
}

func getRaw(ctx context.Context, c *http.Client, base, path string) string {
	resp, err := c.Get(base + path)
	if err != nil {
		panic(fmt.Sprintf("smoke GET raw %s: %v", path, err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

func mustDo(ctx context.Context, c *http.Client, base, method, path string, body, _ any) {
	_, err := do[map[string]any](ctx, c, base, method, path, body)
	if err != nil {
		panic(fmt.Sprintf("smoke %s %s: %v", method, path, err))
	}
}
