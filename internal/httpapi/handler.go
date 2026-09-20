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

// route binds a method and a ServeMux pattern to its handler.
type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

// routes is the complete HTTP surface of the service in one place: the mux is
// built from this table, and contract_test.go checks it against the swag
// annotations that the release step turns into the registered OpenAPI document.
// A route missing here is missing from the contract; an annotation without a
// route here would advertise an endpoint that answers 404.
func (h *Handler) routes() []route {
	return []route{
		// /health is the deployment platform's uniform probe path; /healthz is
		// kept as an alias for the platform-independent convention.
		{"GET", "/health", h.health},
		{"GET", "/healthz", h.healthz},

		{"GET", basePath + "/departments", h.listDepartments},
		{"POST", basePath + "/departments", h.createDepartment},
		{"GET", basePath + "/departments/{id}", h.getDepartment},
		{"PATCH", basePath + "/departments/{id}", h.renameDepartment},

		{"GET", basePath + "/persons", h.listPersons},
		{"POST", basePath + "/persons", h.registerPerson},
		{"GET", basePath + "/persons/{id}", h.getPerson},
	}
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
	for _, rt := range h.routes() {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}

	// Browser UI (embedded single page + assets). "/" is exact-matched so it
	// cannot shadow the API routes above.
	mountUI(mux)

	return h.recoverPanic(h.logRequests(mux))
}

// health reports liveness. It is the probe the deployment platform polls, so
// its path and payload are part of the platform contract.
//
// @Summary  健康检查
// @Tags     system
// @Produce  json
// @Success  200  {object}  healthResponse
// @Router   /health [get]
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Service: serviceName,
		Version: h.version,
	})
}

// createDepartment handles POST /api/v1/departments.
//
// @Summary  新增部门
// @Tags     departments
// @Accept   json
// @Produce  json
// @Param    request  body      org.CreateDepartmentRequest  true  "部门名称与类型（研发/测试/产品/管理，支持英文别名）"
// @Success  201      {object}  org.Department
// @Failure  400      {object}  errorResponse  "参数校验失败"
// @Failure  409      {object}  errorResponse  "部门名称已存在"
// @Router   /api/v1/departments [post]
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

// healthz is the path-agnostic convention alias of /health.
//
// @Summary  健康检查（/health 的别名）
// @Tags     system
// @Produce  json
// @Success  200  {object}  healthResponse
// @Router   /healthz [get]
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	h.health(w, r)
}

// listDepartments handles GET /api/v1/departments.
//
// @Summary  部门列表
// @Tags     departments
// @Produce  json
// @Success  200  {object}  departmentListResponse
// @Router   /api/v1/departments [get]
func (h *Handler) listDepartments(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, departmentListResponse{
		Items: h.svc.ListDepartments(),
		Types: org.DepartmentTypeNames(),
	})
}

// getDepartment handles GET /api/v1/departments/{id}.
//
// @Summary  部门详情（含下属人员）
// @Tags     departments
// @Produce  json
// @Param    id   path      string  true  "部门 ID，如 D0001"
// @Success  200  {object}  org.DepartmentDetail
// @Failure  404  {object}  errorResponse  "部门不存在"
// @Router   /api/v1/departments/{id} [get]
func (h *Handler) getDepartment(w http.ResponseWriter, r *http.Request) {
	detail, err := h.svc.GetDepartment(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// renameDepartment handles PATCH /api/v1/departments/{id}: it renames an
// existing department while keeping its id, type and members.
//
// @Summary  部门重命名
// @Tags     departments
// @Accept   json
// @Produce  json
// @Param    id       path      string                      true  "部门 ID，如 D0001"
// @Param    request  body      org.RenameDepartmentRequest  true  "新名称"
// @Success  200      {object}  org.Department
// @Failure  400      {object}  errorResponse  "参数校验失败"
// @Failure  404      {object}  errorResponse  "部门不存在"
// @Failure  409      {object}  errorResponse  "部门名称已存在"
// @Router   /api/v1/departments/{id} [patch]
func (h *Handler) renameDepartment(w http.ResponseWriter, r *http.Request) {
	var req org.RenameDepartmentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	dept, err := h.svc.RenameDepartment(r.PathValue("id"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dept)
}

// registerPerson handles POST /api/v1/persons.
//
// @Summary  人员注册（HUMAN / AGENT）
// @Tags     persons
// @Accept   json
// @Produce  json
// @Param    request  body      org.RegisterPersonRequest  true  "人员名称 / 人员 ID / 类型 / 所在部门"
// @Success  201      {object}  org.PersonView  "注册成功，含系统分配的工号"
// @Failure  400      {object}  errorResponse   "参数校验失败"
// @Failure  404      {object}  errorResponse   "部门不存在"
// @Failure  409      {object}  errorResponse   "人员 ID 已存在"
// @Router   /api/v1/persons [post]
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
//
// @Summary  人员列表
// @Tags     persons
// @Produce  json
// @Param    departmentId  query     string  false  "按部门过滤，如 D0001"
// @Success  200           {object}  personListResponse
// @Failure  404           {object}  errorResponse  "过滤用的部门不存在"
// @Router   /api/v1/persons [get]
func (h *Handler) listPersons(w http.ResponseWriter, r *http.Request) {
	persons, err := h.svc.ListPersons(r.URL.Query().Get("departmentId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, personListResponse{
		Items: persons,
		Types: org.PersonTypeNames(),
	})
}

// getPerson handles GET /api/v1/persons/{id}; id may be a person id or an
// employee number.
//
// @Summary  人员信息（按人员 ID 或工号）
// @Tags     persons
// @Produce  json
// @Param    id   path      string  true  "人员 ID（如 agent-001）或工号（如 E0001）"
// @Success  200  {object}  org.PersonView
// @Failure  404  {object}  errorResponse  "人员不存在"
// @Router   /api/v1/persons/{id} [get]
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
	writeJSON(w, status, errorResponse{Error: errorBody{
		Kind:    string(org.KindOf(err)),
		Message: err.Error(),
	}})
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
				writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{
					Kind:    "internal",
					Message: "internal server error",
				}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
