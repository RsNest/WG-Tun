package engine

import (
	"context"
	"fmt"
	"strings"

	"transitforge/internal/firewall"
	"transitforge/internal/logging"
	"transitforge/internal/model"
	"transitforge/internal/reconcile"
	"transitforge/internal/wireguard"
)

type Engine struct {
	FW  *firewall.IptablesNftManager
	WG  *wireguard.WGManager
	Log *logging.Logger
	HP  HaproxyApplier
	SSH SSHApplier
}

type HaproxyApplier interface {
	Discover(ctx context.Context, desired []model.SniRoute) (digest string, routes []model.SniRoute, conflicts []model.Conflict, err error)
	Apply(ctx context.Context, desired []model.SniRoute, backends []model.Backend) error
	Rollback(ctx context.Context) error
}

type SSHApplier interface {
	Discover(ctx context.Context, t model.Tunnel) (model.TunnelActual, []model.Conflict, error)
	Apply(ctx context.Context, t model.Tunnel) error
}

func (e *Engine) Discover(ctx context.Context, desired model.DesiredState) (model.ActualState, error) {
	st := model.ActualState{NodeID: desired.Node.ID}
	if e.FW != nil {
		rules, conflicts, err := e.FW.Discover(ctx)
		if err != nil {
			return st, err
		}
		st.FirewallRules = rules
		st.Conflicts = append(st.Conflicts, conflicts...)
	}
	if e.WG != nil {
		tuns, conflicts, err := e.WG.Discover(ctx, desired.Tunnels)
		if err != nil {
			return st, err
		}
		st.Tunnels = append(st.Tunnels, tuns...)
		st.Conflicts = append(st.Conflicts, conflicts...)
	}
	if e.SSH != nil {
		for _, t := range desired.Tunnels {
			if t.Type != model.TunnelSSHTUN {
				continue
			}
			act, conflicts, err := e.SSH.Discover(ctx, t)
			if err != nil {
				return st, err
			}
			st.Tunnels = append(st.Tunnels, act)
			st.Conflicts = append(st.Conflicts, conflicts...)
		}
	}
	if e.HP != nil {
		digest, routes, conflicts, err := e.HP.Discover(ctx, desired.SniRoutes)
		if err != nil {
			return st, err
		}
		st.HaproxyDigest = digest
		st.SniRoutes = routes
		st.Conflicts = append(st.Conflicts, conflicts...)
	}
	return st, nil
}

type Result struct {
	Plan    reconcile.Plan
	Actual  model.ActualState
	Applied bool
	Rolled  bool
}

func (e *Engine) Reconcile(ctx context.Context, desired model.DesiredState, dryRun bool) (Result, error) {
	actual, err := e.Discover(ctx, desired)
	if err != nil {
		return Result{}, err
	}
	plan := reconcile.Diff(desired, actual)
	res := Result{Plan: plan, Actual: actual}
	if plan.HasConflicts() {
		return res, model.ErrConflict(plan.String())
	}
	if plan.Empty() || dryRun {
		return res, nil
	}
	if err := e.validatePlan(plan, desired); err != nil {
		return res, err
	}
	if e.WG != nil {
		e.WG.BeginTx()
	}
	if err := e.applyPlan(ctx, plan, desired); err != nil {
		if e.Log != nil {
			e.Log.Error("apply failed, rolling back", logging.Fields{Event: logging.EventRollbackStarted, Error: err.Error()})
		}
		if rbErr := e.rollback(ctx); rbErr != nil {
			return Result{Plan: plan, Actual: actual, Rolled: false}, fmt.Errorf("apply failed: %v; rollback failed: %w", err, rbErr)
		}
		if e.Log != nil {
			e.Log.Info("rollback completed", logging.Fields{Event: logging.EventRollbackCompleted})
		}
		return Result{Plan: plan, Actual: actual, Rolled: true}, fmt.Errorf("apply failed, rolled back: %w", err)
	}
	after, err := e.Discover(ctx, desired)
	if err != nil {
		_ = e.rollback(ctx)
		return Result{Plan: plan, Actual: actual, Rolled: true}, fmt.Errorf("verify discover failed, rolled back: %w", err)
	}
	res.Actual = after
	verify := reconcile.Diff(desired, after)
	if !verify.Empty() || verify.HasConflicts() {
		_ = e.rollback(ctx)
		return Result{Plan: plan, Actual: after, Rolled: true}, model.ErrConflict("verify failed after apply:\n" + verify.String())
	}
	res.Applied = true
	return res, nil
}

func (e *Engine) validatePlan(plan reconcile.Plan, desired model.DesiredState) error {
	byID := map[string]model.Tunnel{}
	for _, t := range desired.Tunnels {
		byID[string(t.ID)] = t
	}
	for _, a := range plan.Actions {
		switch a.Target {
		case "firewall":
			if e.FW == nil {
				return model.NotImplemented("firewall manager is not configured")
			}
		case "tunnel":
			t := byID[a.ID]
			if a.Kind == reconcile.KindDelete {
				continue
			}
			switch t.Type {
			case model.TunnelWireGuard:
				if e.WG == nil {
					return model.NotImplemented("wireguard manager is not configured")
				}
			case model.TunnelSSHTUN:
				if e.SSH == nil {
					return model.NotImplemented("SSH_TUN apply is not enabled")
				}
			default:
				return model.NotImplemented("unknown tunnel type")
			}
		case "haproxy":
			if e.HP == nil {
				return model.NotImplemented("HAProxy manager is not configured")
			}
		default:
			return model.NotImplemented("unknown plan target " + a.Target)
		}
	}
	return nil
}

func (e *Engine) applyPlan(ctx context.Context, plan reconcile.Plan, desired model.DesiredState) error {
	byID := map[string]model.Tunnel{}
	for _, t := range desired.Tunnels {
		byID[string(t.ID)] = t
	}
	for _, a := range plan.Actions {
		if a.Target != "tunnel" {
			continue
		}
		switch a.Kind {
		case reconcile.KindAdd, reconcile.KindChange:
			t, ok := byID[a.ID]
			if !ok {
				return fmt.Errorf("plan references missing tunnel %s", a.ID)
			}
			if t.Type == model.TunnelWireGuard {
				if err := e.WG.Apply(ctx, t); err != nil {
					return err
				}
			} else if t.Type == model.TunnelSSHTUN {
				if err := e.SSH.Apply(ctx, t); err != nil {
					return err
				}
			}
		case reconcile.KindDelete:
			iface := ifaceFromBefore(a.Before)
			if iface == "" {
				return fmt.Errorf("cannot delete tunnel without interface name")
			}
			if err := e.WG.Delete(ctx, iface); err != nil {
				return err
			}
		}
	}
	if e.HP != nil {
		need := false
		for _, a := range plan.Actions {
			if a.Target == "haproxy" {
				need = true
				break
			}
		}
		if need {
			if err := e.HP.Apply(ctx, desired.SniRoutes, desired.Backends); err != nil {
				return err
			}
		}
	}
	if e.FW != nil {
		fwPlan := reconcile.Plan{}
		for _, a := range plan.Actions {
			if a.Target == "firewall" {
				fwPlan.Actions = append(fwPlan.Actions, a)
			}
		}
		if !fwPlan.Empty() {
			if err := e.FW.Apply(ctx, fwPlan, desired.Mappings, desired.Backends); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) rollback(ctx context.Context) error {
	var errs []string
	if e.HP != nil {
		if err := e.HP.Rollback(ctx); err != nil {
			errs = append(errs, "haproxy: "+err.Error())
		}
	}
	if e.FW != nil {
		if err := e.FW.Rollback(ctx); err != nil {
			errs = append(errs, "firewall: "+err.Error())
		}
	}
	if e.WG != nil {
		for _, iface := range e.WG.CreatedIfaces() {
			if err := e.WG.RollbackCreate(ctx, iface); err != nil {
				errs = append(errs, "wireguard "+iface+": "+err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func ifaceFromBefore(s string) string {
	const p = "if="
	i := strings.Index(s, p)
	if i < 0 {
		return ""
	}
	rest := s[i+len(p):]
	f := strings.Fields(rest)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
