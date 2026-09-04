package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML-friendly time.Duration that accepts values like "10s".
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err == nil && s != "" {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	var ms int64
	if err := n.Decode(&ms); err != nil {
		return fmt.Errorf("duration must be a string like 10s")
	}
	*d = Duration(time.Duration(ms) * time.Millisecond)
	return nil
}
