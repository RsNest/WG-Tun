package failover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"transitforge/internal/model"
)

const stateVersion = 1

type File struct {
	Version   int                    `json:"version"`
	UpdatedAt time.Time              `json:"updated_at"`
	Entries   []model.TransportState `json:"entries"`
}

func StatePath(dir string) string {
	return filepath.Join(dir, "transport-state.json")
}

func Load(dir string) (*File, error) {
	p := StatePath(dir)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Version: stateVersion, Entries: []model.TransportState{}}, nil
		}
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("transport-state.json: %w", err)
	}
	if f.Entries == nil {
		f.Entries = []model.TransportState{}
	}
	return &f, nil
}

func Save(dir string, f *File) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f.Version = stateVersion
	f.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := StatePath(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath(dir))
}

func (f *File) Get(node, backend model.ID) model.TransportState {
	for _, e := range f.Entries {
		if e.NodeID == node && e.BackendID == backend {
			return e
		}
	}
	return model.TransportState{
		NodeID:    node,
		BackendID: backend,
		State:     model.TransportWGPrimary,
	}
}

func (f *File) Put(st model.TransportState) {
	for i, e := range f.Entries {
		if e.NodeID == st.NodeID && e.BackendID == st.BackendID {
			f.Entries[i] = st
			return
		}
	}
	f.Entries = append(f.Entries, st)
}
