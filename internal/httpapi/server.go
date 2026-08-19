package httpapi

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	RFID    *ingress.RFID
	LPR     *ingress.LPR
	Version string
	GitSHA  string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok", "service": "plantops-edge-scale-go", "version": s.Version,
			"git_sha": s.GitSHA, "utc": time.Now().UTC(),
		})
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
