package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"transitforge/internal/agent"
	"transitforge/internal/client"
	"transitforge/internal/cmdexec"
	"transitforge/internal/config"
	"transitforge/internal/engine"
	"transitforge/internal/failover"
	"transitforge/internal/firewall"
	"transitforge/internal/haproxy"
	"transitforge/internal/health"
	"transitforge/internal/logging"
	"transitforge/internal/metrics"
	"transitforge/internal/sshtun"
	"transitforge/internal/version"
	"transitforge/internal/wireguard"
)

func main() {
	if maybeVersion() {
		return
	}
	if maybeHealthcheck() {
		return
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "agent: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "configs/example-agent.yaml", "path to agent YAML config")
	insecure := flag.Bool("insecure", false, "skip TLS verify (lab only)")
	flag.Parse()

	cfg, err := config.LoadAgent(*cfgPath)
	if err != nil {
		return err
	}
	tok, err := agent.LoadToken(cfg.TokenFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.StateDir, 0o750); err != nil {
		return err
	}
	log := logging.New("agent")
	cli := client.New(cfg.ControllerURL, tok, *insecure || cfg.TLS.InsecureSkipVerify)
	cli.UserAgent = agent.UserAgent()
	live := !cfg.DryRunOnly
	a := agent.New(cfg, cli, log, agent.EmptyDiscoverer{}, nil, live)
	runner := cmdexec.OSCommandRunner{Timeout: 30 * time.Second}
	ssh := &sshtun.Manager{Runner: runner}
	eng := &engine.Engine{
		FW:  &firewall.IptablesNftManager{Runner: runner, BackupDir: filepath.Join(cfg.StateDir, "backups")},
		WG:  &wireguard.WGManager{Runner: runner},
		HP:  &haproxy.Manager{Runner: runner, ConfigPath: cfg.HaproxyConfig, BackupDir: filepath.Join(cfg.StateDir, "haproxy-backups"), ReloadMode: cfg.HaproxyReload},
		SSH: ssh,
		Log: log,
	}
	met := metrics.New()
	a.SetMetrics(met)
	a.SetEngine(eng)
	a.SetFailover(&failover.Controller{
		Dir:    cfg.StateDir,
		Engine: eng,
		SSH:    ssh,
		Health: &health.Checker{Runner: runner},
		Log:    log,
		Live:   live,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.MetricsListen != "" {
		go func() {
			if err := agent.ServeHTTP(ctx, cfg.MetricsListen, a, met); err != nil && ctx.Err() == nil {
				log.Error("metrics server", logging.Fields{Error: err.Error()})
			}
		}()
	}
	log.Info("agent starting", logging.Fields{Extra: map[string]any{"node": cfg.NodeName, "version": version.Version, "live_apply": live}})
	return a.Run(ctx)
}
