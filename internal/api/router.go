// Package api wires the HTTP gateway: the JSON API under /api/v1 and the embedded web UI.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/searchboard/searchboard/internal/tsdb"
	"github.com/searchboard/searchboard/web"
)

// Deps are the collaborators the API needs. All fields except Metrics are required.
type Deps struct {
	Log       *slog.Logger
	Clusters  ClusterRepository
	Connector Connector
	Metrics   tsdb.MetricsStore
}

// server holds the handlers' shared dependencies.
type server struct {
	log       *slog.Logger
	clusters  ClusterRepository
	connector Connector
}

// NewRouter builds the HTTP handler for the whole application.
func NewRouter(d Deps) http.Handler {
	s := &server{log: d.Log, clusters: d.Clusters, connector: d.Connector}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})

		r.Route("/clusters", func(r chi.Router) {
			r.Get("/", s.listClusters)
			r.Post("/", s.createCluster)
			r.Route("/{id}", func(r chi.Router) {
				r.Delete("/", s.deleteCluster)
				r.Get("/matrix", s.getMatrix)
				r.Get("/allocation-explain", s.getAllocationExplain)
			})
		})
	})

	r.Handle("/*", spaHandler())
	return r
}

// spaHandler serves the embedded Vite build and falls back to index.html so
// client-side routes survive a page reload.
func spaHandler() http.Handler {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err) // the embedded directory is fixed at compile time
	}
	files := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(sub, name); err != nil {
			req.URL.Path = "/"
		}
		files.ServeHTTP(w, req)
	})
}
