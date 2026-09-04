package failover

import (
	"time"

	"transitforge/internal/health"
	"transitforge/internal/logging"
	"transitforge/internal/model"
)

type Input struct {
	Current     model.TransportState
	Policy      model.FailoverPolicy
	Primary     health.Snapshot
	Fallback    health.Snapshot
	FailForward bool
	Now         time.Time
}

type Decision struct {
	Next         model.TransportState
	CutoverToSSH bool
	CutoverToWG  bool
	Critical     bool
	Event        string
	Reason       string
}

func Step(in Input) Decision {
	pol := in.Policy
	pol.ApplyDefaults()
	st := in.Current
	if st.State == "" {
		st.State = model.TransportWGPrimary
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	st.PrimaryHealthy = !health.PrimaryFailed(in.Primary, 2)
	st.FallbackHealthy = health.FallbackHealthy(in.Fallback)
	st.UpdatedAt = in.Now

	d := Decision{Next: st}

	switch st.State {
	case model.TransportWGPrimary:
		if st.PrimaryHealthy {
			st.ConsecutiveFailures = 0
			d.Next = st
			return d
		}
		st.ConsecutiveFailures++
		if st.ConsecutiveFailures < pol.FailureThreshold {
			st.LastReason = "primary unhealthy, counting failures"
			d.Next = st
			return d
		}
		if !st.FallbackHealthy {
			st.State = model.TransportDegraded
			st.LastReason = "CRITICAL_NO_HEALTHY_TRANSPORT"
			st.LastTransition = in.Now
			d.Next = st
			d.Critical = true
			d.Event = logging.EventTransportDegraded
			d.Reason = st.LastReason
			return d
		}
		if !pol.AutomaticFailback {
			st.LastReason = "threshold reached but automatic_failback is false"
			d.Next = st
			return d
		}
		st.State = model.TransportFailbackInProgress
		st.LastTransition = in.Now
		st.LastReason = "automatic failback to SSH_TUN"
		d.Next = st
		d.CutoverToSSH = true
		d.Event = logging.EventFailbackStarted
		d.Reason = st.LastReason
		return d

	case model.TransportFailbackInProgress:
		if st.FallbackHealthy {
			st.State = model.TransportSSHPrimary
			st.LastTransition = in.Now
			st.LastReason = "failback completed"
			st.ConsecutiveFailures = 0
			d.Next = st
			d.Event = logging.EventFailbackCompleted
			return d
		}
		st.State = model.TransportDegraded
		st.LastReason = "CRITICAL_NO_HEALTHY_TRANSPORT"
		st.LastTransition = in.Now
		d.Next = st
		d.Critical = true
		d.Event = logging.EventTransportDegraded
		return d

	case model.TransportSSHPrimary:
		if in.FailForward {
			if st.PrimaryHealthy {
				st.State = model.TransportWGPrimary
				st.LastTransition = in.Now
				st.LastReason = "operator fail-forward"
				st.ConsecutiveFailures = 0
				d.Next = st
				d.CutoverToWG = true
				d.Event = logging.EventFailbackCompleted
				d.Reason = st.LastReason
				return d
			}
			st.LastReason = "operator fail-forward requested but WireGuard primary is not healthy"
			d.Next = st
			return d
		}
		if !st.FallbackHealthy && !st.PrimaryHealthy {
			st.State = model.TransportDegraded
			st.LastReason = "CRITICAL_NO_HEALTHY_TRANSPORT"
			st.LastTransition = in.Now
			d.Critical = true
			d.Event = logging.EventTransportDegraded
			d.Next = st
			return d
		}
		if !st.FallbackHealthy && st.PrimaryHealthy {
			st.State = model.TransportDegraded
			st.LastReason = "SSH_TUN unhealthy; fail-forward requires operator action"
			st.LastTransition = in.Now
			d.Event = logging.EventTransportDegraded
			d.Next = st
			return d
		}
		if pol.AutomaticFailforward && st.PrimaryHealthy {
			st.LastReason = "automatic_failforward is ignored unless explicitly requested via API; staying SSH_PRIMARY"
			d.Next = st
			return d
		}
		d.Next = st
		return d

	case model.TransportDegraded:
		if in.FailForward && st.PrimaryHealthy {
			st.State = model.TransportWGPrimary
			st.LastTransition = in.Now
			st.LastReason = "operator fail-forward from DEGRADED"
			st.ConsecutiveFailures = 0
			d.Next = st
			d.CutoverToWG = true
			return d
		}
		if pol.AutomaticFailback && !st.PrimaryHealthy && st.FallbackHealthy {
			st.State = model.TransportFailbackInProgress
			st.LastTransition = in.Now
			st.LastReason = "recovering via SSH_TUN from DEGRADED"
			d.Next = st
			d.CutoverToSSH = true
			d.Event = logging.EventFailbackStarted
			return d
		}
		d.Next = st
		return d
	}
	d.Next = st
	return d
}
