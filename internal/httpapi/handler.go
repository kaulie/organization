// Package httpapi exposes the organization service as a small JSON/HTTP API.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/kaulie/organization/internal/org"
)

const (
	basePath     = "/api/v1"
	maxBodyBytes = 1 << 20 // 1 MiB
	// serviceName is reported by the health endpoint.
	serviceName = "organization"
)

// Handler serves the organization API.
type Handler struct {
	svc     *org.Service
	log     *slog.Logger
	version string
}

// Option customizes the handler.
type Option func(*Handler)

// WithVersion sets the version reported by the health endpoint (deployment
// injects APP_VERSION; the build may also bake it in via -ldflags).
func WithVersion(v string) Option {
	return func(h *Handler) { h.version = v }
}

// New builds the HTTP handler for the organization API. A nil logger falls
// back to slog.Default().
func New(svc *org.Service, logger *slog.Logger, opts ...Option) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{svc: svc, log: logger, version: "dev"}
	for _, opt := range opts {
		opt(h)
	}

	mux := http.NewServeMux()
	// /health is the deployment platform's uniform probe path; /healthz is kept
	// as an alias for the platform-independent convention.
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET "+basePath+"/departments", h.listDepartments)
	mux.HandleFunc("POST "+basePath+"/departments", h.createDepartment)
	mux.HandleFunc("GET "+basePath+"/departments/{id}", h.getDepartment)
	mux.HandleFunc("GET "+basePath+"/persons", h.listPersons)
	mux.HandleFunc("POST "+basePath+"/persons", h.registerPerson)
	mux.HandleFunc("GET "+basePath+"/persons/{id}", h.getPerson)

	// Browser UI (embedded single page + assets). "/" is exact-matched so it
	// cannot shadow the API routes above.
	mountUI(mux)

	return h.recoverPanic(h.logRequests(mux))
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": serviceName,
		"version": h.version,
	})
}

// createDepartment handles POST /api/v1/departments.
func (h *Handler) createDepartment(w http.ResponseWriter, r *http.Request) {
	var req org.CreateDepartmentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	dept, err := h.svc.CreateDepartment(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dept)
}

// listDepartments handles GET /api/v1/departments.
func (h *Handler) listDepartments(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": h.svc.ListDepartments(),
		"types": org.DepartmentTypeNames(),
	})
}

// getDepartment handles GET /api/v1/departments/{id}.
func (h *Handler) getDepartment(w http.ResponseWriter, r *http.Request) {
	detail, err := h.svc.GetDepartment(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// registerPerson handles POST /api/v1/persons.
func (h *Handler) registerPerson(w http.ResponseWriter, r *http.Request) {
	var req org.RegisterPersonRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	person, err := h.svc.RegisterPerson(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, person)
}

// listPersons handles GET /api/v1/persons?departmentId=...
func (h *Handler) listPersons(w http.ResponseWriter, r *http.Request) {
	persons, err := h.svc.ListPersons(r.URL.Query().Get("departmentId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": persons,
		"types": org.PersonTypeNames(),
	})
}

// getPerson handles GET /api/v1/persons/{id}; id may be a person id or an
// employee number.
func (h *Handler) getPerson(w http.ResponseWriter, r *http.Request) {
	person, err := h.svc.GetPerson(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, person)
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return org.Validationf("request body must be a JSON object")
		}
		return org.Validationf("invalid JSON body: %v", err)
	}
	if decoder.More() {
		return org.Validationf("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"kind":"internal","message":"failed to encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, err error) {
	status := statusFor(err)
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"kind":    string(org.KindOf(err)),
			"message": err.Error(),
		},
	})
}

func statusFor(err error) int {
	switch org.KindOf(err) {
	case org.KindValidation:
		return http.StatusBadRequest
	case org.KindNotFound:
		return http.StatusNotFound
	case org.KindConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (h *Handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		h.log.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).String(),
		)
	})
}

func (h *Handler) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				h.log.Error("panic", "path", r.URL.Path, "recover", rec)
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]string{"kind": "internal", "message": "internal server error"},
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
