// Package api wires the HTTP / WebSocket / SSE gateway and embedded web UI.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/searchboard/searchboard/internal/store"
	"github.com/searchboard/searchboard/internal/tsdb"
	"github.com/searchboard/searchboard/web"
)

type Deps struct {
	Log     *slog.Logger
	Store   *store.Store
	Metrics tsdb.MetricsStore
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		// TODO: /clusters, /clusters/{id}/proxy (DevTools), /metrics, /logs (SSE/WS),
		//       /migrations, /inspections
	})

	r.Handle("/*", spaHandler())
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// spaHandler serves the embedded Vite build, falling back to index.html for client-side routes.
func spaHandler() http.Handler {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := strings.TrimPrefix(req.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			req.URL.Path = "/"
		}
		files.ServeHTTP(w, req)
	})
}
