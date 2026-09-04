package failover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LockTimeout is how long Acquire waits for exclusive flock.
const LockTimeout = 5 * time.Second

// LockTTL is the operator-visible heartbeat budget for lock metadata.
// The kernel drops flock when the holder process exits, so a dead holder
// does not require breaking the lock. We never steal a live flock.
const LockTTL = 30 * time.Second

type lockMeta struct {
	PID         int       `json:"pid"`
	AcquiredAt  time.Time `json:"acquired_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	TTLSeconds  int       `json:"ttl_seconds"`
}

type Lock struct {
	Path    string
	Timeout time.Duration
	file    *os.File
}

func NewLock(dir string) *Lock {
	return &Lock{
		Path:    filepath.Join(dir, "transport.lock"),
		Timeout: LockTimeout,
	}
}

func (l *Lock) Acquire(ctx context.Context) error {
	if l.Timeout == 0 {
		l.Timeout = LockTimeout
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o750); err != nil {
		return err
	}
	deadline := time.Now().Add(l.Timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := lockFile(f); err == nil {
			l.file = f
			_ = l.writeMeta()
			return nil
		}
		_ = f.Close()
		if time.Now().After(deadline) {
			meta := l.readMeta()
			return modelLockTimeout(meta)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	_ = unlockFile(l.file)
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Lock) Heartbeat() error {
	if l.file == nil {
		return fmt.Errorf("lock not held")
	}
	return l.writeMeta()
}

func (l *Lock) writeMeta() error {
	m := lockMeta{PID: os.Getpid(), AcquiredAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC(), TTLSeconds: int(LockTTL.Seconds())}
	b, _ := json.Marshal(m)
	if err := l.file.Truncate(0); err != nil {
		return err
	}
	if _, err := l.file.Seek(0, 0); err != nil {
		return err
	}
	_, err := l.file.Write(b)
	return err
}

func (l *Lock) readMeta() lockMeta {
	b, err := os.ReadFile(l.Path)
	if err != nil {
		return lockMeta{}
	}
	var m lockMeta
	_ = json.Unmarshal(b, &m)
	return m
}

func modelLockTimeout(m lockMeta) error {
	msg := fmt.Sprintf("could not acquire flock on transport.lock within %s (ttl=%s); holder_pid=%d heartbeat=%s — not stealing a live lock",
		LockTimeout, LockTTL, m.PID, m.HeartbeatAt.Format(time.RFC3339))
	return fmt.Errorf("LOCK_TIMEOUT: %s", msg)
}
