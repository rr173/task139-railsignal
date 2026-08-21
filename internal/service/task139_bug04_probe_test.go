package service

import ("context"; "testing"; "task139-railsignal/internal/model")

func TestBug04_TerminalClearanceCompletesRelease(t *testing.T) {
	svc:=newServiceWithYard(t); ctx:=context.Background(); sig, _, _, _, normal, _:=task139BugIDs(t,svc)
	res,err:=svc.RequestRoute(ctx,RouteRequest{OriginSignalID:sig,TerminalSectionID:normal}); if err!=nil {t.Fatal(err)}
	for _,id:=range res.Route.PathSections { _,_=svc.ReportOccupancy(ctx,OccupancyEvent{SectionID:id}) }
	for _,id:=range res.Route.PathSections { _,_=svc.ReportClearance(ctx,ClearanceEvent{SectionID:id}) }
	r,_:=svc.GetRoute(ctx,res.Route.ID); if r.State!=model.RouteReleased {t.Fatalf("state=%s, want RELEASED",r.State)}
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

