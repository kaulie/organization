package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kaulie/organization/internal/org"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	svc := org.NewService(org.NewMemoryStore())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(svc, logger))
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode %s %s response %q: %v", method, url, raw, err)
		}
	}
	return resp.StatusCode, out
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	status, body := doJSON(t, http.MethodGet, srv.URL+"/healthz", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v, want status ok", body)
	}
}

// The deployment platform probes /health on every service; /healthz stays as an
// alias. Both must report the deployment version.
func TestHealthEndpointPlatformPaths(t *testing.T) {
	svc := org.NewService(org.NewMemoryStore())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(svc, logger, WithVersion("deadbeef")))
	t.Cleanup(srv.Close)

	for _, path := range []string{"/health", "/healthz"} {
		status, body := doJSON(t, http.MethodGet, srv.URL+path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, status)
		}
		if body["status"] != "ok" {
			t.Errorf("%s status field = %v, want ok", path, body["status"])
		}
		if body["version"] != "deadbeef" {
			t.Errorf("%s version = %v, want deadbeef", path, body["version"])
		}
		if body["service"] != "organization" {
			t.Errorf("%s service = %v, want organization", path, body["service"])
		}
	}
}

func TestDepartmentAndPersonFlow(t *testing.T) {
	srv := newTestServer(t)

	// 1. create department
	status, dept := doJSON(t, http.MethodPost, srv.URL+"/api/v1/departments",
		map[string]string{"name": "研发中心", "type": "研发"})
	if status != http.StatusCreated {
		t.Fatalf("create department status = %d (%v), want 201", status, dept)
	}
	deptID, _ := dept["id"].(string)
	if deptID == "" {
		t.Fatalf("department id missing: %v", dept)
	}
	if dept["type"] != "研发" {
		t.Errorf("type = %v, want 研发", dept["type"])
	}

	// 2. register a human and an agent
	status, human := doJSON(t, http.MethodPost, srv.URL+"/api/v1/persons",
		map[string]string{"name": "Alice", "id": "user-001", "type": "human", "departmentId": deptID})
	if status != http.StatusCreated {
		t.Fatalf("register human status = %d (%v), want 201", status, human)
	}
	if human["employeeNo"] != "E0001" {
		t.Errorf("employeeNo = %v, want E0001", human["employeeNo"])
	}
	if human["type"] != string(org.PersonTypeHuman) {
		t.Errorf("type = %v, want HUMAN", human["type"])
	}

	status, agent := doJSON(t, http.MethodPost, srv.URL+"/api/v1/persons",
		map[string]string{"name": "Cline", "id": "agent-001", "type": "AGENT", "departmentId": deptID})
	if status != http.StatusCreated {
		t.Fatalf("register agent status = %d (%v), want 201", status, agent)
	}

	// 3. department view includes members
	status, detail := doJSON(t, http.MethodGet, srv.URL+"/api/v1/departments/"+deptID, nil)
	if status != http.StatusOK {
		t.Fatalf("get department status = %d, want 200", status)
	}
	members, _ := detail["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members = %v, want 2 entries", detail["members"])
	}
	first, _ := members[0].(map[string]any)
	if first["departmentName"] != "研发中心" {
		t.Errorf("member departmentName = %v, want 研发中心", first["departmentName"])
	}

	// 4. person view by id and by employee number
	status, person := doJSON(t, http.MethodGet, srv.URL+"/api/v1/persons/user-001", nil)
	if status != http.StatusOK {
		t.Fatalf("get person status = %d, want 200", status)
	}
	if person["name"] != "Alice" || person["departmentId"] != deptID {
		t.Errorf("unexpected person payload: %v", person)
	}

	status, byNo := doJSON(t, http.MethodGet, srv.URL+"/api/v1/persons/E0001", nil)
	if status != http.StatusOK {
		t.Fatalf("get person by employee no status = %d, want 200", status)
	}
	if byNo["id"] != "user-001" {
		t.Errorf("employee number lookup returned %v, want user-001", byNo["id"])
	}

	// 5. list endpoints
	status, list := doJSON(t, http.MethodGet, srv.URL+"/api/v1/persons?departmentId="+deptID, nil)
	if status != http.StatusOK {
		t.Fatalf("list persons status = %d, want 200", status)
	}
	items, _ := list["items"].([]any)
	if len(items) != 2 {
		t.Errorf("listed persons = %v, want 2", list["items"])
	}

	status, depts := doJSON(t, http.MethodGet, srv.URL+"/api/v1/departments", nil)
	if status != http.StatusOK {
		t.Fatalf("list departments status = %d, want 200", status)
	}
	if d, _ := depts["items"].([]any); len(d) != 1 {
		t.Errorf("listed departments = %v, want 1", depts["items"])
	}
}

func TestErrorStatusMapping(t *testing.T) {
	srv := newTestServer(t)

	_, dept := doJSON(t, http.MethodPost, srv.URL+"/api/v1/departments",
		map[string]string{"name": "研发中心", "type": "研发"})
	deptID, _ := dept["id"].(string)

	cases := []struct {
		name       string
		method     string
		path       string
		body       any
		wantStatus int
		wantKind   string
	}{
		{"invalid department type", http.MethodPost, "/api/v1/departments",
			map[string]string{"name": "财务部", "type": "finance"}, http.StatusBadRequest, "validation"},
		{"duplicate department", http.MethodPost, "/api/v1/departments",
			map[string]string{"name": "研发中心", "type": "研发"}, http.StatusConflict, "conflict"},
		{"invalid person type", http.MethodPost, "/api/v1/persons",
			map[string]string{"name": "Bob", "id": "u1", "type": "robot", "departmentId": deptID}, http.StatusBadRequest, "validation"},
		{"unknown department on register", http.MethodPost, "/api/v1/persons",
			map[string]string{"name": "Bob", "id": "u2", "type": "human", "departmentId": "D9999"}, http.StatusNotFound, "not_found"},
		{"unknown department view", http.MethodGet, "/api/v1/departments/D9999", nil, http.StatusNotFound, "not_found"},
		{"unknown person view", http.MethodGet, "/api/v1/persons/nobody", nil, http.StatusNotFound, "not_found"},
		{"unknown field", http.MethodPost, "/api/v1/departments",
			map[string]string{"name": "产品部", "type": "产品", "bogus": "x"}, http.StatusBadRequest, "validation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doJSON(t, tc.method, srv.URL+tc.path, tc.body)
			if status != tc.wantStatus {
				t.Fatalf("status = %d (%v), want %d", status, body, tc.wantStatus)
			}
			errObj, ok := body["error"].(map[string]any)
			if !ok {
				t.Fatalf("missing error object in %v", body)
			}
			if errObj["kind"] != tc.wantKind {
				t.Errorf("kind = %v, want %s", errObj["kind"], tc.wantKind)
			}
		})
	}

	// duplicate person id
	if status, _ := doJSON(t, http.MethodPost, srv.URL+"/api/v1/persons",
		map[string]string{"name": "Alice", "id": "user-001", "type": "human", "departmentId": deptID}); status != http.StatusCreated {
		t.Fatalf("register person status = %d, want 201", status)
	}
	status, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/persons",
		map[string]string{"name": "Alice2", "id": "user-001", "type": "agent", "departmentId": deptID})
	if status != http.StatusConflict {
		t.Fatalf("duplicate person status = %d (%v), want 409", status, body)
	}

	// malformed JSON body
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/departments", bytes.NewBufferString("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed JSON status = %d, want 400", resp.StatusCode)
	}
}
