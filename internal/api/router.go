// Package api wires the HTTP gateway: the JSON API under /api/v1 and the embedded web UI.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/middleware"
	"github.com/searchboard/searchboard/internal/tsdb"
	"github.com/searchboard/searchboard/web"
)

// apiPrefix is the URL prefix of every JSON endpoint.
const apiPrefix = "/api/"

// Deps are the collaborators the API needs. All fields except Metrics and Log are required.
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

// New builds the HTTP server for the whole application: the JSON API under
// /api/v1 and the embedded web UI for every other path. Extra options, such as
// the listen address, are passed to the framework.
func New(d Deps, opts ...framework.Option) *framework.Server {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	s := &server{log: log, clusters: d.Clusters, connector: d.Connector}

	base := []framework.Option{framework.WithLogger(log), framework.WithMaxBodyBytes(maxBodyBytes)}
	app := framework.New(append(base, opts...)...)
	app.Use(middleware.RequestID(), middleware.AccessLog(log), middleware.Recovery(log))

	v1 := app.Group(apiPrefix + "v1")
	v1.GET("/healthz", func(_ context.Context, c *framework.Context) {
		c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	clusters := v1.Group("/clusters")
	clusters.GET("", s.listClusters)
	clusters.POST("", s.createCluster)
	clusters.DELETE("/:id", s.deleteCluster)
	clusters.GET("/:id/matrix", s.getMatrix)
	clusters.GET("/:id/allocation-explain", s.getAllocationExplain)

	app.NoMethod(func(_ context.Context, c *framework.Context) {
		writeError(c, http.StatusMethodNotAllowed, "method not allowed")
	})

	assets, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err) // the embedded directory is fixed at compile time
	}
	app.NoRoute(notFoundOrWebUI(assets))
	return app
}

// notFoundOrWebUI answers unknown API paths with a JSON 404 and serves the web
// UI for everything else.
func notFoundOrWebUI(assets fs.FS) framework.HandlerFunc {
	webUI := spaHandler(assets)
	return func(ctx context.Context, c *framework.Context) {
		if strings.HasPrefix(c.Path(), apiPrefix) {
			writeError(c, http.StatusNotFound, "not found")
			return
		}
		webUI(ctx, c)
	}
}
