// Package httpapi exposes the railway signal engine over HTTP+JSON using the
// Go 1.22 enhanced ServeMux. It serves both the live server and the in-process
// smoke test (which mounts the same mux on a httptest.Server). The handlers
// are thin: they decode the request, call the service, and render a uniform
// JSON body. Engine errors (model.Error) become 4xx + {"error","code"}.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"task139-railsignal/internal/model"
	"task139-railsignal/internal/service"
	"task139-railsignal/internal/store"
	"task139-railsignal/internal/webfs"
)

// New returns an http.Handler for the given service.
func New(svc *service.Service) http.Handler {
	mux := http.NewServeMux()
	h := &handlers{svc: svc}
	h.register(mux)
	return mux
}

// NewFromStore opens the service from a store and returns its handler.
func NewFromStore(st *store.Store) http.Handler {
	return New(service.New(st))
}

type handlers struct {
	svc *service.Service
}

// register wires all routes onto the mux.
func (h *handlers) register(mux *http.ServeMux) {
	// frontend
	mux.HandleFunc("GET /", h.serveIndex)
	mux.HandleFunc("GET /static/app.js", h.serveStatic)
	mux.HandleFunc("GET /static/style.css", h.serveStatic)

	// layout
	mux.HandleFunc("POST /nodes", h.addNode)
	mux.HandleFunc("GET /nodes", h.listNodes)
	mux.HandleFunc("POST /sections", h.addSection)
	mux.HandleFunc("GET /sections", h.listSections)
	mux.HandleFunc("GET /sections/{id}", h.getSection)
	mux.HandleFunc("POST /points", h.addPoint)
	mux.HandleFunc("GET /points", h.listPoints)
	mux.HandleFunc("GET /points/{id}", h.getPoint)
	mux.HandleFunc("POST /points/{id}/operate", h.operatePoint)
	mux.HandleFunc("POST /points/{id}/bypass", h.bypassPoint)
	mux.HandleFunc("POST /signals", h.addSignal)
	mux.HandleFunc("GET /signals", h.listSignals)
	mux.HandleFunc("GET /signals/{id}", h.getSignal)
	mux.HandleFunc("POST /signals/{id}/set-red", h.setSignalRed)

	mux.HandleFunc("GET /layout", h.layout)
	mux.HandleFunc("POST /layout/reset", h.resetLayout)

	// routes + train moves
	mux.HandleFunc("POST /routes", h.requestRoute)
	mux.HandleFunc("GET /routes", h.listRoutes)
	mux.HandleFunc("GET /routes/{id}", h.getRoute)
	mux.HandleFunc("POST /routes/{id}/cancel", h.cancelRoute)
	mux.HandleFunc("POST /occupancy", h.occupancy)
	mux.HandleFunc("POST /clearance", h.clearance)
	mux.HandleFunc("POST /clock/advance", h.advanceClock)

	// reports + recovery
	mux.HandleFunc("POST /reconcile", h.reconcile)
	mux.HandleFunc("GET /reports/interlocking", h.interlockingReport)
	mux.HandleFunc("GET /reports/conflicts", h.conflictReport)
	mux.HandleFunc("GET /reports/audit", h.auditReport)
	mux.HandleFunc("GET /events", h.events)
}

// writeJSON renders v as JSON, or an engine error.
func writeJSON(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr maps an error to a uniform JSON body.
func writeErr(w http.ResponseWriter, err error) {
	se := model.AsError(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(se.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": se.Message, "code": string(se.Code)})
}

// decode reads a JSON body into dst.
func decode[T any](r *http.Request) (T, error) {
	var dst T
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&dst); err != nil {
			return dst, model.Invariantf("invalid JSON body: %v", err)
		}
	}
	return dst, nil
}

// ---------------------------------------------------------------------------
// frontend
// ---------------------------------------------------------------------------

func (h *handlers) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := webfs.FS().ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "frontend missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (h *handlers) serveStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	b, err := webfs.FS().ReadFile("web/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch name {
	case "app.js":
		w.Header().Set("Content-Type", "application/javascript")
	case "style.css":
		w.Header().Set("Content-Type", "text/css")
	}
	_, _ = w.Write(b)
}

// ---------------------------------------------------------------------------
// layout handlers
// ---------------------------------------------------------------------------

func (h *handlers) addNode(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.AddNodeRequest](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	n, err := h.svc.AddNode(r.Context(), req)
	writeJSON(w, n, err)
}

func (h *handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.ListNodes(r.Context()), nil)
}

func (h *handlers) addSection(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.AddSectionRequest](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s, err := h.svc.AddSection(r.Context(), req)
	writeJSON(w, s, err)
}

func (h *handlers) listSections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.ListSections(r.Context()), nil)
}

func (h *handlers) getSection(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.GetSection(r.Context(), r.PathValue("id"))
	writeJSON(w, s, err)
}

func (h *handlers) addPoint(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.AddPointRequest](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	p, err := h.svc.AddPoint(r.Context(), req)
	writeJSON(w, p, err)
}

func (h *handlers) listPoints(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.ListPoints(r.Context()), nil)
}

func (h *handlers) getPoint(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPoint(r.Context(), r.PathValue("id"))
	writeJSON(w, p, err)
}

func (h *handlers) operatePoint(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Direction model.PointDirection `json:"direction"`
	}](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	p, err := h.svc.OperatePoint(r.Context(), r.PathValue("id"), body.Direction)
	writeJSON(w, p, err)
}

func (h *handlers) bypassPoint(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		On bool `json:"on"`
	}](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	p, err := h.svc.BypassPoint(r.Context(), r.PathValue("id"), body.On)
	writeJSON(w, p, err)
}

func (h *handlers) addSignal(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.AddSignalRequest](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s, err := h.svc.AddSignal(r.Context(), req)
	writeJSON(w, s, err)
}

func (h *handlers) listSignals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.ListSignals(r.Context()), nil)
}

func (h *handlers) getSignal(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.GetSignal(r.Context(), r.PathValue("id"))
	writeJSON(w, s, err)
}

func (h *handlers) setSignalRed(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.SetSignalRed(r.Context(), r.PathValue("id"))
	writeJSON(w, s, err)
}

func (h *handlers) layout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.Layout(r.Context()), nil)
}

func (h *handlers) resetLayout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "reset"}, h.svc.ResetLayout(r.Context()))
}

// ---------------------------------------------------------------------------
// routes + train moves
// ---------------------------------------------------------------------------

func (h *handlers) requestRoute(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.RouteRequest](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.svc.RequestRoute(r.Context(), req)
	writeJSON(w, res, err)
}

func (h *handlers) listRoutes(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	writeJSON(w, h.svc.ListRoutes(r.Context(), state), nil)
}

func (h *handlers) getRoute(w http.ResponseWriter, r *http.Request) {
	rt, err := h.svc.GetRoute(r.Context(), r.PathValue("id"))
	writeJSON(w, rt, err)
}

func (h *handlers) cancelRoute(w http.ResponseWriter, r *http.Request) {
	rt, err := h.svc.CancelRoute(r.Context(), r.PathValue("id"))
	writeJSON(w, rt, err)
}

func (h *handlers) occupancy(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.OccupancyEvent](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.svc.ReportOccupancy(r.Context(), req)
	writeJSON(w, res, err)
}

func (h *handlers) clearance(w http.ResponseWriter, r *http.Request) {
	req, err := decode[service.ClearanceEvent](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.svc.ReportClearance(r.Context(), req)
	writeJSON(w, res, err)
}

func (h *handlers) advanceClock(w http.ResponseWriter, r *http.Request) {
	body, err := decode[struct {
		Delta int `json:"delta"`
	}](r)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := h.svc.AdvanceClock(r.Context(), body.Delta)
	writeJSON(w, res, err)
}

func (h *handlers) reconcile(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Reconcile(r.Context())
	writeJSON(w, res, err)
}

func (h *handlers) interlockingReport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.InterlockingReport(r.Context()), nil)
}

func (h *handlers) conflictReport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.svc.ConflictReportAll(r.Context()), nil)
}

func (h *handlers) auditReport(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rep, err := h.svc.AuditReport(r.Context(), limit, offset)
	writeJSON(w, rep, err)
}

func (h *handlers) events(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rep, err := h.svc.AuditReport(r.Context(), limit, offset)
	writeJSON(w, rep, err)
}
