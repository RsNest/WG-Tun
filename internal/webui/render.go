package webui

import (
	"embed"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"time"

	"proxyctl/internal/model"
)

//go:embed templates/*.html static/app.css static/htmx.min.js static/LICENSE-htmx.txt
var content embed.FS

type page struct {
	Title     string
	Nav       string
	Principal *model.PrincipalView
	CanWrite  bool
	FlashOK   string
	FlashErr  string
	Partial   bool
	Data      any
}

func parseTemplates() (*template.Template, error) {
	return template.New("").ParseFS(content, "templates/*.html")
}

func (s *Server) render(w http.ResponseWriter, _ *http.Request, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, p); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func writePlain(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}

func hx(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

func relTime(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.UTC().Sub(t.UTC())
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s ago"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}

func fmtBytes(n uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return strconv.FormatUint(n/gb, 10) + " GiB"
	case n >= mb:
		return strconv.FormatUint(n/mb, 10) + " MiB"
	case n >= kb:
		return strconv.FormatUint(n/kb, 10) + " KiB"
	default:
		return strconv.FormatUint(n, 10) + " B"
	}
}

func fmtHandshake(sec int64) string {
	if sec <= 0 {
		return "—"
	}
	if sec < 60 {
		return strconv.FormatInt(sec, 10) + "s"
	}
	if sec < 3600 {
		return strconv.FormatInt(sec/60, 10) + "m"
	}
	return strconv.FormatInt(sec/3600, 10) + "h"
}
