package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"proxyctl/internal/client"
	"proxyctl/internal/config"
	"proxyctl/internal/engine"
	"proxyctl/internal/failover"
	"proxyctl/internal/logging"
	"proxyctl/internal/metrics"
	"proxyctl/internal/model"
	"proxyctl/internal/reconcile"
	"proxyctl/internal/version"
)

type Discoverer interface {
	Discover(ctx context.Context, desired model.DesiredState) (model.ActualState, error)
}

type Applier interface {
	Apply(ctx context.Context, plan reconcile.Plan, desired model.DesiredState) error
}

type Agent struct {
	cfg        *config.AgentConfig
	cli        *client.Client
	log        *logging.Logger
	discoverer Discoverer
	applier    Applier
	nodeID     model.ID
	liveApply  bool
	engine     *engine.Engine
	failover   *failover.Controller
	metrics    *metrics.Metrics
	ready      bool
}

func New(cfg *config.AgentConfig, cli *client.Client, log *logging.Logger, d Discoverer, a Applier, liveApply bool) *Agent {
	if d == nil {
		d = EmptyDiscoverer{}
	}
	return &Agent{cfg: cfg, cli: cli, log: log.WithNode(cfg.NodeName), discoverer: d, applier: a, liveApply: liveApply && !cfg.DryRunOnly}
}

func (a *Agent) SetEngine(e *engine.Engine) {
	a.engine = e
}

func (a *Agent) SetFailover(c *failover.Controller) {
	a.failover = c
}

func (a *Agent) SetMetrics(m *metrics.Metrics) {
	a.metrics = m
}

func (a *Agent) SetReady(v bool) { a.ready = v }

type EmptyDiscoverer struct{}

func (EmptyDiscoverer) Discover(_ context.Context, desired model.DesiredState) (model.ActualState, error) {
	return model.ActualState{NodeID: desired.Node.ID, DiscoveredAt: time.Now().UTC()}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.resolveNode(ctx); err != nil {
		return err
	}
	a.SetReady(true)
	t := time.NewTicker(a.cfg.ReconcileInterval.Duration())
	defer t.Stop()
	if err := a.tick(ctx); err != nil {
		a.log.Error("reconcile failed", logging.Fields{Event: logging.EventReconcileCompleted, Error: err.Error()})
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := a.tick(ctx); err != nil {
				a.log.Error("reconcile failed", logging.Fields{Event: logging.EventReconcileCompleted, Error: err.Error()})
			}
		}
	}
}

func (a *Agent) resolveNode(ctx context.Context) error {
	nodes, err := a.cli.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}
	for _, n := range nodes {
		if n.Name == a.cfg.NodeName {
			a.nodeID = n.ID
			a.log.Info("agent registered", logging.Fields{Event: logging.EventAgentRegistered, Extra: map[string]any{"node_id": string(n.ID)}})
			return nil
		}
	}
	return fmt.Errorf("node %q is not registered on the controller", a.cfg.NodeName)
}

func (a *Agent) tick(ctx context.Context) error {
	start := time.Now()
	a.log.Info("reconcile started", logging.Fields{Event: logging.EventReconcileStarted})
	ds, err := a.cli.DesiredState(ctx, string(a.nodeID))
	if err != nil {
		return err
	}
	if a.failover != nil {
		states, foErr := a.failover.RunOnce(ctx, *ds, !a.liveApply)
		var actual model.ActualState
		if a.engine != nil {
			actual, _ = a.engine.Discover(ctx, *ds)
			actual.NodeID = a.nodeID
			actual.DiscoveredAt = time.Now().UTC()
			actual.TransportStates = states
			if err := a.cli.PutActualState(ctx, string(a.nodeID), actual); err != nil {
				return fmt.Errorf("report actual-state: %w", err)
			}
		}
		a.observe(foErr == nil, actual)
		if foErr != nil {
			a.log.Error("failover/reconcile failed", logging.Fields{Event: logging.EventReconcileCompleted, Error: foErr.Error(), DurationMS: time.Since(start).Milliseconds()})
			return foErr
		}
		a.log.Info("reconcile completed", logging.Fields{Event: logging.EventReconcileCompleted, DurationMS: time.Since(start).Milliseconds()})
		return nil
	}
	if a.engine != nil {
		res, recErr := a.engine.Reconcile(ctx, *ds, !a.liveApply)
		actual := res.Actual
		actual.NodeID = a.nodeID
		actual.DiscoveredAt = time.Now().UTC()
		if err := a.cli.PutActualState(ctx, string(a.nodeID), actual); err != nil {
			return fmt.Errorf("report actual-state: %w", err)
		}
		a.observe(recErr == nil, actual)
		if recErr != nil {
			a.log.Error("reconcile failed", logging.Fields{Event: logging.EventReconcileCompleted, Error: recErr.Error(), DurationMS: time.Since(start).Milliseconds()})
			return recErr
		}
		if res.Plan.Empty() {
			a.log.Info("reconcile completed", logging.Fields{Event: logging.EventReconcileCompleted, DurationMS: time.Since(start).Milliseconds(), Extra: map[string]any{"result": "NO CHANGES"}})
			return nil
		}
		extra := map[string]any{"plan": res.Plan.String(), "applied": res.Applied}
		a.log.Info("reconcile completed", logging.Fields{Event: logging.EventReconcileCompleted, DurationMS: time.Since(start).Milliseconds(), Extra: extra})
		return nil
	}
	actual, err := a.discoverer.Discover(ctx, *ds)
	if err != nil {
		return err
	}
	actual.NodeID = a.nodeID
	actual.DiscoveredAt = time.Now().UTC()
	plan := reconcile.Diff(*ds, actual)
	if err := a.cli.PutActualState(ctx, string(a.nodeID), actual); err != nil {
		return fmt.Errorf("report actual-state: %w", err)
	}
	if plan.HasConflicts() {
		a.log.Error("reconcile conflict", logging.Fields{Event: logging.EventConflict, Error: plan.String(), DurationMS: time.Since(start).Milliseconds()})
		return model.ErrConflict(plan.String())
	}
	if plan.Empty() {
		a.log.Info("reconcile completed", logging.Fields{Event: logging.EventReconcileCompleted, DurationMS: time.Since(start).Milliseconds(), Extra: map[string]any{"result": "NO CHANGES"}})
		return nil
	}
	if !a.liveApply || a.applier == nil {
		a.log.Info("dry-run plan (not applying)", logging.Fields{Event: logging.EventReconcileCompleted, DurationMS: time.Since(start).Milliseconds(), Extra: map[string]any{"plan": plan.String()}})
		return nil
	}
	if err := a.applier.Apply(ctx, plan, *ds); err != nil {
		return err
	}
	a.log.Info("reconcile completed", logging.Fields{Event: logging.EventReconcileCompleted, DurationMS: time.Since(start).Milliseconds()})
	return nil
}

func LoadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return tok, nil
}

func UserAgent() string { return "proxyctl-agent/" + version.Version }

func (a *Agent) observe(success bool, actual model.ActualState) {
	if a.metrics == nil {
		return
	}
	a.metrics.ObserveReconcile(a.cfg.NodeName, success, actual)
}

func (a *Agent) Ready() bool {
	return a.ready && a.engine != nil
}
