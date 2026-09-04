package failover

import (
	"context"
	"fmt"
	"time"

	"transitforge/internal/engine"
	"transitforge/internal/health"
	"transitforge/internal/logging"
	"transitforge/internal/model"
	"transitforge/internal/sshtun"
)

type Controller struct {
	Dir    string
	Engine *engine.Engine
	SSH    *sshtun.Manager
	Health *health.Checker
	Log    *logging.Logger
	Live   bool
}

func (c *Controller) RunOnce(ctx context.Context, desired model.DesiredState, dryRun bool) ([]model.TransportState, error) {
	lock := NewLock(c.Dir)
	if err := lock.Acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()

	file, err := Load(c.Dir)
	if err != nil {
		return nil, err
	}
	prev := *file
	live := c.Live && !dryRun

	eff := applyStoredOverlays(desired, file)
	if c.Engine != nil {
		if _, err := c.Engine.Reconcile(ctx, eff, !live); err != nil {
			return nil, err
		}
	}

	var out []model.TransportState

	for _, b := range desired.Backends {
		wgTun, sshTun := splitTunnels(desired, b.ID)
		if wgTun == nil {
			continue
		}
		pol := policyFor(desired, b.ID)
		cur := file.Get(desired.Node.ID, b.ID)
		prim, err := c.primaryHealth(ctx, *wgTun, b, desired)
		if err != nil {
			return nil, err
		}
		var fb health.Snapshot
		if sshTun != nil {
			fb, err = c.fallbackHealth(ctx, *sshTun, b, desired)
			if err != nil {
				return nil, err
			}
		}
		dec := Step(Input{
			Current:     cur,
			Policy:      pol,
			Primary:     prim,
			Fallback:    fb,
			FailForward: hasIntent(desired, b.ID),
			Now:         time.Now().UTC(),
		})
		if c.Log != nil && dec.Event != "" {
			lvl := c.Log.Info
			if dec.Critical {
				lvl = c.Log.Error
			}
			lvl(dec.Reason, logging.Fields{
				Event:     dec.Event,
				Backend:   b.Name,
				Transport: string(dec.Next.State),
				Extra:     map[string]any{"reason": dec.Reason},
			})
		}
		if live && (dec.CutoverToSSH || dec.CutoverToWG) {
			if err := c.cutover(ctx, desired, b, wgTun, sshTun, dec); err != nil {
				if rb := Save(c.Dir, &prev); rb != nil && c.Log != nil {
					c.Log.Error("failed to restore transport state after cutover error", logging.Fields{Error: rb.Error()})
				}
				return nil, err
			}
		}
		file.Put(dec.Next)
		out = append(out, dec.Next)
	}
	if err := Save(c.Dir, file); err != nil {
		return out, err
	}
	_ = lock.Heartbeat()
	return out, nil
}

func (c *Controller) cutover(ctx context.Context, desired model.DesiredState, b model.Backend, wgTun, sshTun *model.Tunnel, dec Decision) error {
	eff := desired
	switch {
	case dec.CutoverToSSH:
		if sshTun == nil {
			return model.ErrConflict("SSH_TUN tunnel missing; cannot fail back")
		}
		if c.SSH != nil {
			if err := c.SSH.Apply(ctx, *sshTun); err != nil {
				return err
			}
		}
		fb, err := c.fallbackHealth(ctx, *sshTun, b, desired)
		if err != nil {
			return err
		}
		if !health.FallbackHealthy(fb) {
			return model.ErrConflict("SSH fallback is not healthy; refusing cutover")
		}
		eff = rewriteBackendAddr(desired, b.ID, sshTun.RemoteOverlayIP)
	case dec.CutoverToWG:
		if wgTun == nil {
			return model.ErrConflict("WIREGUARD tunnel missing; cannot fail forward")
		}
		eff = rewriteBackendAddr(desired, b.ID, wgTun.RemoteOverlayIP)
	}
	if c.Engine == nil {
		return fmt.Errorf("cutover engine not configured")
	}
	res, err := c.Engine.Reconcile(ctx, eff, false)
	if err != nil {
		if c.Engine != nil {
			// Reconcile already rolls back firewall/HAProxy on apply failure.
			_ = res
		}
		return err
	}
	return nil
}

func (c *Controller) primaryHealth(ctx context.Context, t model.Tunnel, b model.Backend, ds model.DesiredState) (health.Snapshot, error) {
	h := health.Snapshot{}
	if c.Engine != nil && c.Engine.WG != nil {
		acts, _, err := c.Engine.WG.Discover(ctx, []model.Tunnel{t})
		if err != nil {
			return h, err
		}
		if len(acts) > 0 {
			h.InterfacePresent = acts[0].InterfacePresent
			h.HandshakeAgeSec = acts[0].HandshakeAgeSec
		}
	}
	if c.Health != nil {
		ok, _ := c.Health.OverlayPing(ctx, t.RemoteOverlayIP)
		h.OverlayReachable = ok
		c.probe(ctx, &h, b, ds, t.RemoteOverlayIP)
	}
	return h, nil
}

func (c *Controller) fallbackHealth(ctx context.Context, t model.Tunnel, b model.Backend, ds model.DesiredState) (health.Snapshot, error) {
	h := health.Snapshot{}
	if c.SSH != nil {
		act, _, err := c.SSH.Discover(ctx, t)
		if err != nil {
			return h, err
		}
		h.InterfacePresent = act.InterfacePresent
		h.UnitActive = act.ServiceActive
	}
	if c.Health != nil {
		ok, _ := c.Health.OverlayPing(ctx, t.RemoteOverlayIP)
		h.OverlayReachable = ok
		c.probe(ctx, &h, b, ds, t.RemoteOverlayIP)
	}
	return h, nil
}

func (c *Controller) probe(ctx context.Context, h *health.Snapshot, b model.Backend, ds model.DesiredState, addr string) {
	for _, m := range ds.Mappings {
		if m.BackendID != b.ID || m.Protocol != model.ProtoTCP {
			continue
		}
		h.ProbesTotal++
		ok, _ := c.Health.ProbeBackend(ctx, addr, m.Protocol, m.BackendPort)
		if !ok {
			h.ProbesFailed++
		}
	}
}

func splitTunnels(ds model.DesiredState, backend model.ID) (wg, ssh *model.Tunnel) {
	for i := range ds.Tunnels {
		t := ds.Tunnels[i]
		if t.BackendID != backend {
			continue
		}
		cp := t
		switch t.Type {
		case model.TunnelWireGuard:
			wg = &cp
		case model.TunnelSSHTUN:
			ssh = &cp
		}
	}
	return wg, ssh
}

func policyFor(ds model.DesiredState, backend model.ID) model.FailoverPolicy {
	for _, p := range ds.FailoverPolicies {
		if p.BackendID == backend {
			return p
		}
	}
	p := model.DefaultFailoverPolicy()
	p.NodeID = ds.Node.ID
	p.BackendID = backend
	return p
}

func hasIntent(ds model.DesiredState, backend model.ID) bool {
	for _, in := range ds.FailbackIntents {
		if in.BackendID == backend && (in.Action == "fail_forward" || in.Action == "") {
			return true
		}
	}
	return false
}

func applyStoredOverlays(ds model.DesiredState, f *File) model.DesiredState {
	out := ds
	for _, e := range f.Entries {
		if e.State != model.TransportSSHPrimary && e.State != model.TransportFailbackInProgress {
			continue
		}
		_, sshTun := splitTunnels(ds, e.BackendID)
		if sshTun != nil {
			out = rewriteBackendAddr(out, e.BackendID, sshTun.RemoteOverlayIP)
		}
	}
	return out
}

func rewriteBackendAddr(ds model.DesiredState, backend model.ID, addr string) model.DesiredState {
	out := ds
	out.Backends = append([]model.Backend(nil), ds.Backends...)
	for i := range out.Backends {
		if out.Backends[i].ID == backend {
			out.Backends[i].Address = addr
		}
	}
	return out
}
