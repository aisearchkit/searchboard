// Command searchboard is the Searchboard server.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/internal/api"
	"github.com/searchboard/searchboard/internal/config"
	"github.com/searchboard/searchboard/internal/store"
	"github.com/searchboard/searchboard/internal/tsdb"
)

func main() {
	cfgPath := flag.String("config", "configs/searchboard.yaml", "path to config file")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log, *cfgPath); err != nil {
		log.Error("searchboard failed", "err", err)
		os.Exit(1)
	}
}

// run wires the dependencies and serves until SIGINT or SIGTERM. It returns
// instead of exiting so deferred cleanups (closing the database) always run.
func run(log *slog.Logger, cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	metrics, err := tsdb.New(cfg.TSDB)
	if err != nil {
		return fmt.Errorf("init tsdb: %w", err)
	}
	defer metrics.Close()

	app := api.New(api.Deps{
		Log:       log,
		Clusters:  db,
		Connector: api.NewClientCache(),
		Metrics:   metrics,
	}, framework.WithHostPorts(cfg.Server.Listen))

	log.Info("searchboard listening", "addr", cfg.Server.Listen)
	// Spin serves until a signal arrives, then drains in-flight requests.
	if err := app.Spin(); err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	log.Info("stopped")
	return nil
}
