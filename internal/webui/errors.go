package webui

import (
	"errors"
	"net/http"
	"strings"

	"proxyctl/internal/logging"
	"proxyctl/internal/model"
)

type alertView struct {
	Kind    string
	Title   string
	Message string
}

func classifyUIError(err error) (status int, a alertView) {
	if err == nil {
		return http.StatusInternalServerError, alertView{Kind: "internal", Title: "Controller error", Message: "The controller could not complete this request."}
	}
	var ce *model.CodedError
	if errors.As(err, &ce) && ce != nil {
		msg := safeOperatorMessage(ce.Message)
		switch ce.Code {
		case "UNAUTHORIZED":
			return http.StatusUnauthorized, alertView{Kind: "unauthorized", Title: "Session expired", Message: "Sign in again to continue."}
		case "FORBIDDEN":
			return http.StatusForbidden, alertView{Kind: "forbidden", Title: "Not allowed", Message: firstNonEmpty(msg, "This action requires the operator role.")}
		case "NOT_FOUND":
			return http.StatusNotFound, alertView{Kind: "notfound", Title: "Not found", Message: firstNonEmpty(msg, "The requested resource was not found.")}
		case "CONFLICT":
			return http.StatusConflict, alertView{Kind: "conflict", Title: "Conflict", Message: firstNonEmpty(msg, "The controller reported a conflict.")}
		case "VALIDATION":
			return http.StatusBadRequest, alertView{Kind: "validation", Title: "Invalid request", Message: firstNonEmpty(msg, "The submitted values were not accepted.")}
		case "UNAVAILABLE":
			return http.StatusServiceUnavailable, alertView{Kind: "unavailable", Title: "Controller unavailable", Message: firstNonEmpty(msg, "The controller is not ready.")}
		case "NOT_IMPLEMENTED":
			return http.StatusNotImplemented, alertView{Kind: "unavailable", Title: "Not available", Message: firstNonEmpty(msg, "This action is not enabled on this controller.")}
		case "RATE_LIMIT":
			return http.StatusTooManyRequests, alertView{Kind: "warning", Title: "Too many requests", Message: firstNonEmpty(msg, "Wait a moment and retry.")}
		default:
			return http.StatusInternalServerError, alertView{Kind: "internal", Title: "Controller error", Message: "The controller could not complete this request."}
		}
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "unauthorized") || strings.Contains(low, "unauthorised"):
		return http.StatusUnauthorized, alertView{Kind: "unauthorized", Title: "Session expired", Message: "Sign in again to continue."}
	case strings.Contains(low, "forbidden"):
		return http.StatusForbidden, alertView{Kind: "forbidden", Title: "Not allowed", Message: "This action requires the operator role."}
	case strings.Contains(low, "not found"):
		return http.StatusNotFound, alertView{Kind: "notfound", Title: "Not found", Message: "The requested resource was not found."}
	case strings.Contains(low, "conflict"):
		return http.StatusConflict, alertView{Kind: "conflict", Title: "Conflict", Message: safeOperatorMessage(err.Error())}
	case strings.Contains(low, "validat"):
		return http.StatusBadRequest, alertView{Kind: "validation", Title: "Invalid request", Message: safeOperatorMessage(err.Error())}
	case strings.Contains(low, "connection refused") || strings.Contains(low, "timeout") || strings.Contains(low, "unavailable"):
		return http.StatusBadGateway, alertView{Kind: "unavailable", Title: "API unavailable", Message: "The controller API did not respond."}
	default:
		return http.StatusBadGateway, alertView{Kind: "unavailable", Title: "API unavailable", Message: "The controller API request failed."}
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

func planViewFromError(err error) planView {
	_, a := classifyUIError(err)
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
