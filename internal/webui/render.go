package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"proxyctl/internal/model"
	"proxyctl/internal/webui/i18n"
)

//go:embed templates/*.html static/app.css static/app.js static/htmx.min.js static/LICENSE-htmx.txt
var content embed.FS

type page struct {
	Title        string
	Nav          string
	CrumbGroup   string
	CrumbPage    string
	Principal    *model.PrincipalView
	DisplayName  string
	NavCollapsed bool
	CanWrite     bool
	CanAdmin     bool
	UserID       string
	TokenSession bool
	LiveApply    bool
	Locale       string
	T            func(string) string
	FlashOK      string
	FlashErr     string
	Partial      bool
	Data         any
}

func (p page) Tr(key string) string {
	if key == "" {
		return ""
	}
	if p.T != nil {
		return p.T(key)
	}
	return i18n.T(p.Locale, key)
}

func (p page) Trf(key string, args ...any) string {
	return i18n.Format(p.Locale, key, args...)
}

func parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"dash": func(s string) string {
			if strings.TrimSpace(s) == "" {
				return "—"
			}
			return s
		},
		"statusKey": statusKey,
		"fmtTime":   fmtTime,
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: odd argument count")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: keys must be strings")
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
	}).ParseFS(content, "templates/*.html")
}

func (s *Server) render(w http.ResponseWriter, _ *http.Request, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, p); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *Server) renderStatus(w http.ResponseWriter, _ *http.Request, status int, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, name, p); err != nil {
		return
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

func statusKey(status string) string {
	switch strings.TrimSpace(status) {
	case "unhealthy":
		return "status.unreachable"
	case "":
		return "status.unknown"
	default:
		return "status." + status
	}
}

const navCookie = "proxyctl_nav"

func queryID(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("id"))
}

func queryNew(r *http.Request) bool {
	v := strings.TrimSpace(r.URL.Query().Get("new"))
	return v == "1" || strings.EqualFold(v, "true")
}

func eventsAPIQuery(r *http.Request) string {
	q := url.Values{}
	for _, k := range []string{"node", "backend", "since", "until", "action"} {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			q.Set(k, v)
		}
	}
	enc := q.Encode()
	if enc == "" {
		return ""
	}
	return "?" + enc
}

func retainQuery(r *http.Request, drop ...string) string {
	q := r.URL.Query()
	for _, d := range drop {
		q.Del(d)
	}
	enc := q.Encode()
	if enc == "" {
		return ""
	}
	return "?" + enc
}

func retainQueryAmp(r *http.Request, drop ...string) string {
	s := retainQuery(r, drop...)
	if s == "" {
		return ""
	}
	return "&" + strings.TrimPrefix(s, "?")
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

func relTime(now, t time.Time, locale string) string {
	if t.IsZero() {
		return "—"
	}
	d := now.UTC().Sub(t.UTC())
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Second:
		return i18n.T(locale, "time.just_now")
	case d < time.Minute:
		return i18n.Format(locale, "time.seconds_ago", int(d.Seconds()))
	case d < time.Hour:
		return i18n.Format(locale, "time.minutes_ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.Format(locale, "time.hours_ago", int(d.Hours()))
	default:
		return i18n.Format(locale, "time.days_ago", int(d.Hours()/24))
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
