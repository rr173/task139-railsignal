package service

import ("context"; "testing"; "task139-railsignal/internal/model")

func TestBug09_DivergingRouteShowsCautionAspectAfterRecovery(t *testing.T) {
	svc:=newServiceWithYard(t); ctx:=context.Background(); sig,_,_,_,_,reverse:=task139BugIDs(t,svc)
	if _,err:=svc.RequestRoute(ctx,RouteRequest{OriginSignalID:sig,TerminalSectionID:reverse}); err!=nil {t.Fatal(err)}; if _,err:=svc.AdvanceClock(ctx,5); err!=nil {t.Fatal(err)}; if _,err:=svc.Reconcile(ctx); err!=nil {t.Fatal(err)}
	s,_:=svc.GetSignal(ctx,sig); if s.Aspect!=model.AspectYellow {t.Fatalf("diverging aspect=%s, want YELLOW",s.Aspect)}
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

