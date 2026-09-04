package webui

import (
	"strings"
)

type planLine struct {
	Kind string
	Text string
}

type planView struct {
	NoChanges bool
	Lines     []planLine
	Notice    string
	Error     string
	ErrorKind string
}

func buildPlanView(raw string) planView {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "NO CHANGES") || strings.HasPrefix(strings.ToUpper(trimmed), "NO CHANGES") {
		return planView{NoChanges: true}
	}
	var lines []planLine
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" && len(lines) == 0 {
			continue
		}
		lines = append(lines, planLine{Kind: planLineKind(line), Text: line})
	}
	if len(lines) == 0 {
		return planView{NoChanges: true}
	}
	return planView{Lines: lines}
}

func planLineKind(line string) string {
	s := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(s, "ADD:"):
		return "add"
	case strings.HasPrefix(s, "CHANGE:"):
		return "change"
	case strings.HasPrefix(s, "DELETE:"):
		return "delete"
	case strings.HasPrefix(s, "CONFLICT:"):
		return "conflict"
	case strings.EqualFold(s, "NO CHANGES"):
		return "none"
	default:
		return "text"
	}
}
