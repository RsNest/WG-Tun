package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"transitforge/internal/api"
	"transitforge/internal/auth"
	"transitforge/internal/config"
	"transitforge/internal/logging"
	"transitforge/internal/metrics"
	"transitforge/internal/store"
	"transitforge/internal/version"
	"transitforge/internal/webui"
)

func main() {
	if maybeVersion() {
		return
	}
	if maybeHealthcheck() {
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "controller: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "", "path to controller YAML config")
	listen := flag.String("listen", "", "override listen address")
	uiListen := flag.String("ui-listen", "", "optional operator Web UI (HTTP). Empty disables. Example: 127.0.0.1:8444")
	dataDir := flag.String("data-dir", "", "override data directory")
	plainHTTP := flag.Bool("plain-http", false, "disable TLS (lab/tests only)")
	flag.Parse()

	log := logging.New("controller")
	var cfg *config.ControllerConfig
	var err error
	if *cfgPath != "" {
		cfg, err = config.LoadController(*cfgPath)
	} else {
		cfg = config.DefaultController()
		if err = cfg.Validate(); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *plainHTTP {
		cfg.TLS.Required = false
	}

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}
	dbPath, err := cfg.DBPath()
	if err != nil {
		return err
	}
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer st.Close()

	a := auth.New(st, cfg.Auth.HMACRequired, cfg.Auth.MaxSkew.Duration())
	tokenFile := cfg.Auth.BootstrapTokenFile
	if tokenFile == "" {
		tokenFile = filepath.Join(cfg.DataDir, "bootstrap.token")
	}
	created, err := a.EnsureBootstrapToken(context.Background(), tokenFile)
	if err != nil {
		return err
	}
	if created {
		log.Info("wrote bootstrap operator token", logging.Fields{Event: logging.EventAudit, Extra: map[string]any{"path": tokenFile}})
	}

	met := metrics.New()
	cap := api.Capabilities{LiveApply: false, Failback: true, Metrics: met.Handler()}
	srv := api.New(cfg, st, a, log, cap)
	srv.SetReady(true)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if strings.TrimSpace(*uiListen) != "" {
		ui, err := webui.New(webui.Config{
			Listen:    *uiListen,
			API:       srv.Handler(),
			Auth:      a,
			Accounts:  auth.NewAccounts(st),
			Log:       log,
			LiveApply: cap.LiveApply,
		})
		if err != nil {
			return fmt.Errorf("web ui: %w", err)
		}
		if err := ui.Start(ctx); err != nil {
			return fmt.Errorf("web ui listen: %w", err)
		}
	}

	log.Info("controller starting", logging.Fields{Extra: map[string]any{"listen": cfg.Listen, "tls": cfg.TLS.Required, "ui": *uiListen, "version": version.Version}})
	err = srv.ListenAndServe(ctx)
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
