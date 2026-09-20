package httpapi

import "github.com/kaulie/organization/internal/org"

// The response types below are the API contract: the swaggo annotations next to
// every handler point at them, so the generated OpenAPI document and the
// encoder stay in sync (a field rename is a compile-time concern, not a doc
// that quietly rots). They are referenced from the annotation comments in
// handler.go and cmd/server/main.go.

// healthResponse is the payload of GET /health (and its /healthz alias).
type healthResponse struct {
	Status  string `json:"status" example:"ok"`
	Service string `json:"service" example:"organization"`
	Version string `json:"version" example:"a15d61a5"`
}

// errorBody is the machine readable error payload.
type errorBody struct {
	Kind    string `json:"kind" example:"not_found"`
	Message string `json:"message" example:"department \"D9999\" not found"`
}

// errorResponse wraps every non-2xx JSON response.
type errorResponse struct {
	Error errorBody `json:"error"`
}

// departmentListResponse is the payload of GET /api/v1/departments. Types lists
// the accepted department types, so a client never has to hardcode them.
type departmentListResponse struct {
	Items []org.Department `json:"items"`
	Types []string         `json:"types"`
}

// personListResponse is the payload of GET /api/v1/persons.
type personListResponse struct {
	Items []org.PersonView `json:"items"`
	Types []string         `json:"types"`
}
