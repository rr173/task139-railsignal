package service

import ("context"; "testing"; "task139-railsignal/internal/model")

func TestBug01_DivergingRouteUsesDetectedReverseDirection(t *testing.T) {
	svc := newServiceWithYard(t); ctx := context.Background(); sig, _, _, _, _, reverse := task139BugIDs(t, svc)
	res, err := svc.RequestRoute(ctx, RouteRequest{OriginSignalID: sig, TerminalSectionID: reverse}); if err != nil { t.Fatal(err) }
	if res.Route.State != model.RoutePointsMoving { t.Fatalf("reverse route state=%s, want POINTS_MOVING", res.Route.State) }
	if _, err := svc.AdvanceClock(ctx, 5); err != nil { t.Fatal(err) }
	r, _ := svc.GetRoute(ctx, res.Route.ID); if r.State != model.RouteLocked { t.Fatalf("state=%s, want LOCKED", r.State) }
	s, _ := svc.GetSignal(ctx, sig); if s.Aspect != model.AspectDoubleYellow { t.Fatalf("aspect=%s, want DOUBLE_YELLOW", s.Aspect) }
}

func task139BugIDs(t *testing.T, svc *Service) (signal, point, approach, first, normal, reverse string) {
	t.Helper()
	for _, s := range svc.Layout(context.Background()).Signals { if s.Code == "SIG-A" { signal = s.ID } }
	for _, p := range svc.Layout(context.Background()).Points { if p.Code == "PT1" { point = p.ID } }
	for _, s := range svc.Layout(context.Background()).Sections {
		switch s.Code { case "SEC-AP": approach=s.ID; case "SEC-S1": first=s.ID; case "SEC-TRACKA": normal=s.ID; case "SEC-TRACKB": reverse=s.ID }
	}
	return
}

