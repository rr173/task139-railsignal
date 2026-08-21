package service

import ("context"; "testing")

func TestBug06_AntiSqueezeBlocksRoutePointMovement(t *testing.T) {
	svc:=newServiceWithYard(t); ctx:=context.Background(); sig, pt, approach, _, _, reverse:=task139BugIDs(t,svc)
	p,_:=svc.graph.Point(pt); p.ProtectSections=[]string{approach}
	if _,err:=svc.ReportOccupancy(ctx,OccupancyEvent{SectionID:approach}); err!=nil {t.Fatal(err)}
	if _,err:=svc.RequestRoute(ctx,RouteRequest{OriginSignalID:sig,TerminalSectionID:reverse}); err==nil {t.Fatal("occupied protect section allowed point movement")}
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

