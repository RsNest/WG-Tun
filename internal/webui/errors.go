package webui

import (
	"errors"
	"net/http"
	"strings"

	"proxyctl/internal/logging"
	"proxyctl/internal/model"
	"proxyctl/internal/webui/i18n"
)

type alertView struct {
	Kind       string
	Title      string
	Message    string
	TitleKey   string
	MessageKey string
}

func classifyUIError(err error) (status int, a alertView) {
	if err == nil {
		return http.StatusInternalServerError, alertView{Kind: "internal", TitleKey: "error.controller_title", MessageKey: "error.controller"}
	}
	var ce *model.CodedError
	if errors.As(err, &ce) && ce != nil {
		msg := safeOperatorMessage(ce.Message)
		switch ce.Code {
		case "UNAUTHORIZED":
			return http.StatusUnauthorized, alertView{Kind: "unauthorized", TitleKey: "error.session_title", MessageKey: "error.session_expired"}
		case "FORBIDDEN":
			return http.StatusForbidden, alertView{Kind: "forbidden", TitleKey: "error.not_allowed_title", MessageKey: "error.not_allowed", Message: msg}
		case "NOT_FOUND":
			return http.StatusNotFound, alertView{Kind: "notfound", TitleKey: "error.not_found_title", MessageKey: "error.not_found", Message: msg}
		case "CONFLICT":
			return http.StatusConflict, alertView{Kind: "conflict", TitleKey: "error.conflict_title", MessageKey: "error.conflict", Message: msg}
		case "VALIDATION":
			return http.StatusBadRequest, alertView{Kind: "validation", TitleKey: "error.invalid_title", MessageKey: "error.invalid", Message: msg}
		case "UNAVAILABLE":
			return http.StatusServiceUnavailable, alertView{Kind: "unavailable", TitleKey: "error.unavailable_title", MessageKey: "error.unavailable", Message: msg}
		case "NOT_IMPLEMENTED":
			return http.StatusNotImplemented, alertView{Kind: "unavailable", TitleKey: "error.not_implemented_title", MessageKey: "error.not_implemented", Message: msg}
		case "RATE_LIMIT":
			return http.StatusTooManyRequests, alertView{Kind: "warning", TitleKey: "error.rate_title", MessageKey: "error.rate_limit", Message: msg}
		default:
			return http.StatusInternalServerError, alertView{Kind: "internal", TitleKey: "error.controller_title", MessageKey: "error.controller"}
		}
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "unauthorized") || strings.Contains(low, "unauthorised"):
		return http.StatusUnauthorized, alertView{Kind: "unauthorized", TitleKey: "error.session_title", MessageKey: "error.session_expired"}
	case strings.Contains(low, "forbidden"):
		return http.StatusForbidden, alertView{Kind: "forbidden", TitleKey: "error.not_allowed_title", MessageKey: "error.not_allowed"}
	case strings.Contains(low, "not found"):
		return http.StatusNotFound, alertView{Kind: "notfound", TitleKey: "error.not_found_title", MessageKey: "error.not_found"}
	case strings.Contains(low, "conflict"):
		return http.StatusConflict, alertView{Kind: "conflict", TitleKey: "error.conflict_title", MessageKey: "error.conflict", Message: safeOperatorMessage(err.Error())}
	case strings.Contains(low, "validat"):
		return http.StatusBadRequest, alertView{Kind: "validation", TitleKey: "error.invalid_title", MessageKey: "error.invalid", Message: safeOperatorMessage(err.Error())}
	case strings.Contains(low, "connection refused") || strings.Contains(low, "timeout") || strings.Contains(low, "unavailable"):
		return http.StatusBadGateway, alertView{Kind: "unavailable", TitleKey: "error.api_title", MessageKey: "error.api_unavailable"}
	default:
		return http.StatusBadGateway, alertView{Kind: "unavailable", TitleKey: "error.api_title", MessageKey: "error.api_failed"}
	}
}

func safeOperatorMessage(msg string) string {
	msg = logging.Redact(msg)
	low := strings.ToLower(msg)
	if strings.Contains(low, "token") || strings.Contains(low, "bearer") || strings.Contains(low, "private") || strings.Contains(low, "secret") || strings.Contains(low, "hmac") {
		return "request failed"
	}
	if strings.Contains(msg, "\npanic") || strings.Contains(low, "goroutine") || strings.Contains(low, "stack") {
		return "The controller could not complete this request."
	}
	if len(msg) > 800 {
		return strings.TrimSpace(msg[:800]) + "…"
	}
	return strings.TrimSpace(msg)
}

func (a alertView) resolve(locale string) alertView {
	if a.Title == "" && a.TitleKey != "" {
		a.Title = i18n.T(locale, a.TitleKey)
	}
	if a.Message == "" && a.MessageKey != "" {
		a.Message = i18n.T(locale, a.MessageKey)
	}
	return a
}

func (s *Server) localizeAlert(r *http.Request, a alertView) alertView {
	return a.resolve(s.locale(r))
}

func planViewFromError(err error) planView {
	_, a := classifyUIError(err)
	a = a.resolve("en")
	view := planView{Error: a.Message, ErrorKind: a.Kind}
	var ce *model.CodedError
	if !errors.As(err, &ce) || ce == nil || ce.Code != "CONFLICT" || strings.TrimSpace(ce.Message) == "" {
		return view
	}
	lines := buildPlanView(ce.Message)
	if lines.NoChanges {
		view.Error = safeOperatorMessage(ce.Message)
		view.ErrorKind = "conflict"
		return view
	}
	lines.Error = "The controller reported a conflict."
	lines.ErrorKind = "conflict"
	return lines
}

func safeEventDetail(detail string) string {
	detail = logging.Redact(detail)
	low := strings.ToLower(detail)
	if strings.Contains(low, "token") || strings.Contains(low, "bearer") || strings.Contains(low, "private") || strings.Contains(low, "secret") || strings.Contains(low, "hmac") {
		return ""
	}
	detail = strings.TrimSpace(detail)
	if len(detail) > 240 {
		return detail[:240] + "…"
	}
	return detail
}
