package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/searchboard/searchboard/internal/api"
	"github.com/searchboard/searchboard/internal/config"
	"github.com/searchboard/searchboard/internal/store"
	"github.com/searchboard/searchboard/internal/tsdb"
)

func main() {
	cfgPath := flag.String("config", "configs/searchboard.yaml", "path to config file")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		log.Error("open sqlite", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	metrics, err := tsdb.New(cfg.TSDB)
	if err != nil {
		log.Error("init tsdb", "err", err)
		os.Exit(1)
	}
	defer metrics.Close()

	srv := &http.Server{
		Addr: cfg.Server.Listen,
		Handler: api.NewRouter(api.Deps{
			Log:       log,
			Clusters:  db,
			Connector: api.NewClientCache(),
			Metrics:   metrics,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("searchboard listening", "addr", cfg.Server.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
