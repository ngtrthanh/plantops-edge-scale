package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/auditjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/rawjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/scaleascii"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

//go:embed web/*
var webFS embed.FS

type Workflow interface {
	Snapshot() engine.Snapshot
	ObservePosition(context.Context, domain.PositionSnapshot) error
	ObserveFault(context.Context, domain.Fault) error
	AuthorizeOverride(context.Context, domain.Override) error
	ResetCompleted() error
}

type Server struct {
	RFID            *ingress.RFID
	LPR             *ingress.LPR
	WeightAudit     *rawjournal.Journal
	EventAudit      *auditjournal.Journal
	ScaleMonitor    *scaleascii.Monitor
	Workflow        Workflow
	IOStatus        func() any
	AllowSimulation bool
	Version         string
	GitSHA          string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		payload := map[string]any{"status":"ok","service":"plantops-edge-scale-go","version":s.Version,"git_sha":s.GitSHA,"utc":time.Now().UTC(),"event_audit_configured":s.EventAudit!=nil}
		if s.ScaleMonitor!=nil{payload["scale"]=s.ScaleMonitor.Snapshot()}
		if s.Workflow!=nil{payload["workflow"]=s.Workflow.Snapshot()}
		if s.IOStatus!=nil{payload["io"]=s.IOStatus()}
		writeJSON(w,http.StatusOK,payload)
	})
	mux.HandleFunc("GET /api/workflow",func(w http.ResponseWriter,_ *http.Request){if s.Workflow==nil{writeJSON(w,http.StatusServiceUnavailable,map[string]string{"error":"workflow engine not configured"});return};writeJSON(w,http.StatusOK,s.Workflow.Snapshot())})
	mux.HandleFunc("GET /api/io/status",func(w http.ResponseWriter,_ *http.Request){if s.IOStatus==nil{writeJSON(w,http.StatusOK,map[string]any{"enabled":false});return};writeJSON(w,http.StatusOK,s.IOStatus())})
	mux.HandleFunc("GET /api/scale/status",func(w http.ResponseWriter,_ *http.Request){if s.ScaleMonitor==nil{writeJSON(w,http.StatusOK,map[string]any{"enabled":false});return};writeJSON(w,http.StatusOK,s.ScaleMonitor.Snapshot())})

	mux.HandleFunc("GET /api/audit/weights",func(w http.ResponseWriter,r *http.Request){if s.WeightAudit==nil{writeJSON(w,http.StatusServiceUnavailable,map[string]string{"error":"raw weight audit not configured"});return};records,err:=s.WeightAudit.Tail(parseLimit(r,200));if err!=nil{writeJSON(w,http.StatusInternalServerError,map[string]string{"error":err.Error()});return};writeJSON(w,http.StatusOK,map[string]any{"count":len(records),"records":records})})
	mux.HandleFunc("GET /api/audit/weights/verify",func(w http.ResponseWriter,_ *http.Request){if s.WeightAudit==nil{writeJSON(w,http.StatusServiceUnavailable,map[string]string{"error":"raw weight audit not configured"});return};writeVerify(w,s.WeightAudit.Verify())})
	mux.HandleFunc("GET /api/audit/events",func(w http.ResponseWriter,r *http.Request){if s.EventAudit==nil{writeJSON(w,http.StatusServiceUnavailable,map[string]string{"error":"operational event audit not configured"});return};records,err:=s.EventAudit.Tail(parseLimit(r,200));if err!=nil{writeJSON(w,http.StatusInternalServerError,map[string]string{"error":err.Error()});return};writeJSON(w,http.StatusOK,map[string]any{"count":len(records),"records":records})})
	mux.HandleFunc("GET /api/audit/events/verify",func(w http.ResponseWriter,_ *http.Request){if s.EventAudit==nil{writeJSON(w,http.StatusServiceUnavailable,map[string]string{"error":"operational event audit not configured"});return};writeVerify(w,s.EventAudit.Verify())})

	mux.HandleFunc("GET /api/identity",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusOK,map[string]any{"rfid":s.RFID.Latest(),"lpr":s.LPR.Latest()})})
	mux.HandleFunc("POST /io/rfid",func(w http.ResponseWriter,r *http.Request){var in struct{Tag string `json:"tag"`;Quality float64 `json:"quality"`};if json.NewDecoder(r.Body).Decode(&in)!=nil||in.Tag==""{writeJSON(w,http.StatusBadRequest,map[string]string{"error":"tag required"});return};s.RFID.Ingest(in.Tag,in.Quality);writeJSON(w,http.StatusAccepted,map[string]string{"status":"accepted"})})
	mux.HandleFunc("POST /io/lpr",func(w http.ResponseWriter,r *http.Request){var in struct{Plate string `json:"plate"`;Confidence float64 `json:"confidence"`;ImageRef string `json:"image_ref"`};if json.NewDecoder(r.Body).Decode(&in)!=nil||in.Plate==""{writeJSON(w,http.StatusBadRequest,map[string]string{"error":"plate required"});return};s.LPR.Ingest(in.Plate,in.Confidence,in.ImageRef);writeJSON(w,http.StatusAccepted,map[string]string{"status":"accepted"})})

	if s.AllowSimulation&&s.Workflow!=nil{
		mux.HandleFunc("POST /sim/position",func(w http.ResponseWriter,r *http.Request){var p domain.PositionSnapshot;if err:=json.NewDecoder(r.Body).Decode(&p);err!=nil{writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()});return};if p.Observed.IsZero(){p.Observed=time.Now().UTC()};if err:=s.Workflow.ObservePosition(r.Context(),p);err!=nil{writeJSON(w,http.StatusConflict,map[string]string{"error":err.Error()});return};writeJSON(w,http.StatusAccepted,s.Workflow.Snapshot())})
		mux.HandleFunc("POST /sim/fault",func(w http.ResponseWriter,r *http.Request){var f domain.Fault;if err:=json.NewDecoder(r.Body).Decode(&f);err!=nil||f.Device==""{writeJSON(w,http.StatusBadRequest,map[string]string{"error":"valid device fault required"});return};if err:=s.Workflow.ObserveFault(r.Context(),f);err!=nil{writeJSON(w,http.StatusConflict,map[string]string{"error":err.Error()});return};writeJSON(w,http.StatusAccepted,s.Workflow.Snapshot())})
		mux.HandleFunc("POST /sim/override",func(w http.ResponseWriter,r *http.Request){var o domain.Override;if err:=json.NewDecoder(r.Body).Decode(&o);err!=nil{writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()});return};if err:=s.Workflow.AuthorizeOverride(r.Context(),o);err!=nil{writeJSON(w,http.StatusConflict,map[string]string{"error":err.Error()});return};writeJSON(w,http.StatusAccepted,s.Workflow.Snapshot())})
		mux.HandleFunc("POST /sim/reset-complete",func(w http.ResponseWriter,_ *http.Request){if err:=s.Workflow.ResetCompleted();err!=nil{writeJSON(w,http.StatusConflict,map[string]string{"error":err.Error()});return};writeJSON(w,http.StatusOK,s.Workflow.Snapshot())})
	}
	static,_:=fs.Sub(webFS,"web");mux.Handle("/",http.FileServer(http.FS(static)));return mux
}

func parseLimit(r *http.Request,fallback int)int{limit:=fallback;if raw:=r.URL.Query().Get("limit");raw!=""{if n,err:=strconv.Atoi(raw);err==nil{limit=n}};return limit}
func writeVerify(w http.ResponseWriter,err error){if err==nil{writeJSON(w,http.StatusOK,map[string]any{"status":"ok","verified":true});return};if os.IsNotExist(err){writeJSON(w,http.StatusOK,map[string]any{"status":"empty","verified":true});return};writeJSON(w,http.StatusConflict,map[string]any{"status":"invalid","verified":false,"error":err.Error()})}
func writeJSON(w http.ResponseWriter,status int,v any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(v)}
