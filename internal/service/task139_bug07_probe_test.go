package service

import ("context"; "testing"; "task139-railsignal/internal/model")

func TestBug07_DetectedPointDirectionPersists(t *testing.T) {
	svc:=newServiceWithYard(t); ctx:=context.Background(); _,pt,_,_,_,_:=task139BugIDs(t,svc)
	if _,err:=svc.OperatePoint(ctx,pt,model.DirReverse); err!=nil {t.Fatal(err)}; if _,err:=svc.AdvanceClock(ctx,5); err!=nil {t.Fatal(err)}; if _,err:=svc.Reconcile(ctx); err!=nil {t.Fatal(err)}
	p,_:=svc.GetPoint(ctx,pt); if p.Direction!=model.DirReverse || p.Status!=model.PointInPosition {t.Fatalf("point recovered as %s/%s",p.Direction,p.Status)}
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

