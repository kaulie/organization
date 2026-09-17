package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The browser UI is embedded in the binary and served from "/" plus "/ui/*".
// This guards the mount contract the single page depends on: the assets must be
// reachable under the /ui/ prefix (StripPrefix), not at the FS root.
func TestUIEndpoints(t *testing.T) {
	srv := newTestServer(t)

	// GET / renders the single page and references the assets it needs.
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET / content-type = %q, want text/html", ct)
	}
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /: %v", err)
	}
	for _, asset := range []string{"/ui/styles.css", "/ui/app.js"} {
		if !bytes.Contains(page, []byte(asset)) {
			t.Errorf("index page does not reference %s", asset)
		}
	}

	// Every asset referenced by the page must be served, not 404.
	for _, asset := range []string{"/ui/styles.css", "/ui/app.js"} {
		resp, err := http.Get(srv.URL + asset)
		if err != nil {
			t.Fatalf("get %s: %v", asset, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", asset, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", asset, resp.StatusCode)
		}
		if len(bytes.TrimSpace(body)) == 0 {
			t.Errorf("GET %s returned an empty body", asset)
		}
	}

	// The UI mount must not shadow the API.
	if status, _ := doJSON(t, http.MethodGet, srv.URL+"/api/v1/departments", nil); status != http.StatusOK {
		t.Errorf("GET /api/v1/departments status = %d, want 200", status)
	}
	// POST / stays 405 (exact match), so the UI cannot swallow API verbs.
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("POST / status = %d, want non-200", resp.StatusCode)
	}
}

// Renaming must be reachable from the browser UI, not only from curl: the page
// ships the rename dialog and app.js drives it through PATCH /departments/{id}.
func TestUIEmbedsRenameControls(t *testing.T) {
	srv := newTestServer(t)

	fetch := func(path string) []byte {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return body
	}

	page := fetch("/")
	for _, needle := range []string{`id="rename-dialog"`, `id="rename-name"`, `id="drawer-rename"`} {
		if !bytes.Contains(page, []byte(needle)) {
			t.Errorf("index page does not expose %s", needle)
		}
	}

	script := fetch("/ui/app.js")
	for _, needle := range []string{"method: 'PATCH'", "data-rename", "openRename"} {
		if !bytes.Contains(script, []byte(needle)) {
			t.Errorf("app.js does not drive the rename flow (%s missing)", needle)
		}
	}
}

// PATCH /api/v1/departments/{id} renames a department in place: the id, type and
// members are preserved, and the new name is subject to the same uniqueness
// rules as creation.
func TestRenameDepartmentEndpoint(t *testing.T) {
	srv := newTestServer(t)

	_, dept := doJSON(t, http.MethodPost, srv.URL+"/api/v1/departments",
		map[string]string{"name": "研发中心", "type": "研发"})
	deptID, _ := dept["id"].(string)
	if deptID == "" {
		t.Fatalf("department id missing: %v", dept)
	}
	if status, person := doJSON(t, http.MethodPost, srv.URL+"/api/v1/persons",
		map[string]string{"name": "Alice", "id": "user-001", "type": "human", "departmentId": deptID}); status != http.StatusCreated {
		t.Fatalf("register person status = %d (%v), want 201", status, person)
	}

	// rename + verify the response shape
	status, renamed := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/departments/"+deptID,
		map[string]string{"name": "平台研发部"})
	if status != http.StatusOK {
		t.Fatalf("rename status = %d (%v), want 200", status, renamed)
	}
	if renamed["name"] != "平台研发部" {
		t.Errorf("name = %v, want 平台研发部", renamed["name"])
	}
	if renamed["id"] != deptID || renamed["type"] != "研发" {
		t.Errorf("id/type must be preserved across a rename: %v", renamed)
	}

	// department detail and the member view both report the new name
	status, detail := doJSON(t, http.MethodGet, srv.URL+"/api/v1/departments/"+deptID, nil)
	if status != http.StatusOK {
		t.Fatalf("get department status = %d, want 200", status)
	}
	if detail["name"] != "平台研发部" {
		t.Errorf("detail name = %v, want 平台研发部", detail["name"])
	}
	members, _ := detail["members"].([]any)
	if len(members) != 1 {
		t.Fatalf("members = %v, want 1", detail["members"])
	}
	member, _ := members[0].(map[string]any)
	if member["departmentId"] != deptID || member["departmentName"] != "平台研发部" {
		t.Errorf("member not rebound to the renamed department: %v", member)
	}

	// another department cannot claim the new name
	_, other := doJSON(t, http.MethodPost, srv.URL+"/api/v1/departments",
		map[string]string{"name": "测试组", "type": "测试"})
	otherID, _ := other["id"].(string)
	status, body := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/departments/"+otherID,
		map[string]string{"name": "平台研发部"})
	if status != http.StatusConflict {
		t.Fatalf("duplicate rename status = %d (%v), want 409", status, body)
	}

	// unknown department -> 404, empty name / unknown field -> 400
	if status, body := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/departments/D404",
		map[string]string{"name": "无人部"}); status != http.StatusNotFound {
		t.Errorf("unknown department status = %d (%v), want 404", status, body)
	}
	if status, body := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/departments/"+deptID,
		map[string]string{"name": "   "}); status != http.StatusBadRequest {
		t.Errorf("empty name status = %d (%v), want 400", status, body)
	}
	if status, body := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/departments/"+deptID,
		map[string]string{"nope": "x"}); status != http.StatusBadRequest {
		t.Errorf("unknown field status = %d (%v), want 400", status, body)
	}
}
