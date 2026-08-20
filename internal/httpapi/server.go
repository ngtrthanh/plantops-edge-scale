package httpapi

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/rawjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/scaleascii"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	RFID         *ingress.RFID
	LPR          *ingress.LPR
	WeightAudit  *rawjournal.Journal
	ScaleMonitor *scaleascii.Monitor
	Version      string
	GitSHA       string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		payload := map[string]any{
			"status": "ok", "service": "plantops-edge-scale-go", "version": s.Version,
			"git_sha": s.GitSHA, "utc": time.Now().UTC(),
		}
		if s.ScaleMonitor != nil {
			payload["scale"] = s.ScaleMonitor.Snapshot()
		}
		writeJSON(w, http.StatusOK, payload)
	})
	mux.HandleFunc("GET /api/scale/status", func(w http.ResponseWriter, _ *http.Request) {
		if s.ScaleMonitor == nil {
			writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
			return
		}
		writeJSON(w, http.StatusOK, s.ScaleMonitor.Snapshot())
	})
	mux.HandleFunc("GET /api/audit/weights", func(w http.ResponseWriter, r *http.Request) {
		if s.WeightAudit == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "raw weight audit not configured"})
			return
		}
		limit := 200
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				limit = n
			}
		}
		records, err := s.WeightAudit.Tail(limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
	mux.HandleFunc("GET /api/audit/weights/verify", func(w http.ResponseWriter, _ *http.Request) {
		if s.WeightAudit == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "raw weight audit not configured"})
			return
		}
		if err := s.WeightAudit.Verify(); err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, map[string]any{"status": "empty", "verified": true})
				return
			}
			writeJSON(w, http.StatusConflict, map[string]any{"status": "invalid", "verified": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "verified": true})
	})
	mux.HandleFunc("GET /api/identity", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"rfid": s.RFID.Latest(), "lpr": s.LPR.Latest()})
	})
	mux.HandleFunc("POST /io/rfid", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Tag     string  `json:"tag"`
			Quality float64 `json:"quality"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Tag == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag required"})
			return
		}
		s.RFID.Ingest(in.Tag, in.Quality)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	})
	mux.HandleFunc("POST /io/lpr", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Plate      string  `json:"plate"`
			Confidence float64 `json:"confidence"`
			ImageRef   string  `json:"image_ref"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Plate == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plate required"})
			return
		}
		s.LPR.Ingest(in.Plate, in.Confidence, in.ImageRef)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	})
	static, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
