package registry

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeOS(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "darwin", want: "mac"},
		{in: "Darwin", want: "mac"},
		{in: "linux", want: "linux"},
		{in: "LINUX", want: "linux"},
		{in: "mac", want: "mac"},
		{in: "windows", want: "windows"},
		{in: "freebsd", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		got, err := normalizeOS(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeOS(%q) error = nil, want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeOS(%q) error = %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("normalizeOS(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeArch(t *testing.T) {
	cases := map[string]string{
		"amd64":   "amd64",
		"x86_64":  "amd64",
		"arm64":   "arm64",
		"aarch64": "arm64",
		"386":     "386",
		"i686":    "386",
		"arm":     "arm",
		"armv7l":  "arm",
		"ppc64":   "",
		"":        "",
	}
	for in, want := range cases {
		if got := normalizeArch(in); got != want {
			t.Errorf("normalizeArch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostForAnnounce(t *testing.T) {
	if got := HostForAnnounce(nil); got != "" {
		t.Errorf("nil host = %q", got)
	}
	if got := HostForAnnounce(net.IPv4zero); got != "" {
		t.Errorf("0.0.0.0 host = %q", got)
	}
	if got := HostForAnnounce(net.IPv6unspecified); got != "" {
		t.Errorf(":: host = %q", got)
	}
	if got := HostForAnnounce(net.ParseIP("127.0.0.1")); got != "127.0.0.1" {
		t.Errorf("loopback = %q", got)
	}
	if got := HostForAnnounce(net.ParseIP("::1")); got != "[::1]" {
		t.Errorf("ipv6 loopback = %q", got)
	}
	if got := HostForAnnounce(net.ParseIP("2001:db8::1")); got != "[2001:db8::1]" {
		t.Errorf("ipv6 = %q", got)
	}
	if got := sanitizeHost("0.0.0.0"); got != "" {
		t.Errorf("sanitize wildcard = %q", got)
	}
	if got := sanitizeHost("::1"); got != "[::1]" {
		t.Errorf("sanitize bare ipv6 = %q", got)
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("REGISTRY_URL", "")
	t.Setenv("REGISTRY_NS", "")
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("REGISTRY_TOKEN", "")
	c := FromEnv()
	if c.BaseURL != defaultRegistryURL || c.Namespace != defaultNamespace || c.Service != defaultService || c.Token != "" {
		t.Fatalf("FromEnv() = %+v", c)
	}

	t.Setenv("REGISTRY_URL", "  http://127.0.0.1:9999/  ")
	t.Setenv("REGISTRY_NS", "  team  ")
	t.Setenv("SERVICE_NAME", " organization ")
	t.Setenv("REGISTRY_TOKEN", " rt_secret ")
	c = FromEnv()
	if c.BaseURL != "http://127.0.0.1:9999/" || c.Namespace != "team" || c.Service != "organization" || c.Token != "rt_secret" {
		t.Fatalf("FromEnv() trimmed = %+v", c)
	}
}

func TestAnnounceCreatedOmitsWildcardHost(t *testing.T) {
	restore := stubRuntime(t, "darwin", "arm64")
	defer restore()

	var gotMethod, gotPath, gotType, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created":  true,
			"instance": map[string]any{"id": "inst_new", "host": "127.0.0.1", "port": 4244},
		})
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL + "/", Namespace: "default", Service: "organization", HTTP: srv.Client()}
	got, err := c.Announce(context.Background(), 4244, "")
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/v1/namespaces/default/services/organization/announce" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotType != "application/json" {
		t.Fatalf("content-type = %q", gotType)
	}
	if gotAuth != "" {
		t.Fatalf("auth = %q, want empty", gotAuth)
	}
	if gotBody["os"] != "mac" || gotBody["arch"] != "arm64" || gotBody["port"] != float64(4244) {
		t.Fatalf("body = %#v", gotBody)
	}
	if _, ok := gotBody["host"]; ok {
		t.Fatalf("wildcard bind must omit host, body = %#v", gotBody)
	}
	if !got.Created || got.ID != "inst_new" || got.Host != "127.0.0.1" || got.Port != 4244 {
		t.Fatalf("announcement = %+v", got)
	}
}

func TestAnnounceSendsHostAndToken(t *testing.T) {
	restore := stubRuntime(t, "linux", "amd64")
	defer restore()

	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"created":false,"instance":{"id":"inst_old","host":"10.0.0.8","port":4244}}`)
	}))
	defer srv.Close()

	c := Client{
		BaseURL:   srv.URL,
		Namespace: "default",
		Service:   "organization",
		Token:     "rt_secret",
		HTTP:      srv.Client(),
	}
	got, err := c.Announce(context.Background(), 4244, "10.0.0.8")
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if gotAuth != "Bearer rt_secret" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody["host"] != "10.0.0.8" || gotBody["os"] != "linux" {
		t.Fatalf("body = %#v", gotBody)
	}
	if got.Created || got.ID != "inst_old" {
		t.Fatalf("announcement = %+v", got)
	}
}

func TestAnnounceRejectsRegistryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"contract missing"}}`)
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Namespace: "default", Service: "organization", HTTP: srv.Client()}
	_, err := c.Announce(context.Background(), 4244, "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "contract missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnnounceRejectsMissingID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"created":true,"instance":{}}`)
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Namespace: "default", Service: "organization", HTTP: srv.Client()}
	_, err := c.Announce(context.Background(), 4244, "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "instance.id") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnnounceDoesNotCallOnBadInput(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Namespace: "default", Service: "organization", HTTP: srv.Client()}

	if _, err := c.Announce(context.Background(), 0, "127.0.0.1"); err == nil {
		t.Fatal("port 0 should fail")
	}
	restore := stubRuntime(t, "freebsd", "amd64")
	defer restore()
	if _, err := c.Announce(context.Background(), 4244, "127.0.0.1"); err == nil {
		t.Fatal("unsupported os should fail")
	}
	if called {
		t.Fatal("invalid announce must not hit the registry")
	}
}

func TestDeregister(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.EscapedPath())
		switch r.URL.Path {
		case "/v1/namespaces/default/services/organization/instances/inst_ok":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/namespaces/default/services/organization/instances/inst_gone":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"gone"}}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"boom"}}`)
		}
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Namespace: "default", Service: "organization", HTTP: srv.Client()}

	if err := c.Deregister(context.Background(), ""); err != nil {
		t.Fatalf("empty id: %v", err)
	}
	if err := c.Deregister(context.Background(), "inst_ok"); err != nil {
		t.Fatalf("204: %v", err)
	}
	if err := c.Deregister(context.Background(), "inst_gone"); err != nil {
		t.Fatalf("404: %v", err)
	}
	if err := c.Deregister(context.Background(), "inst_bad"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("500 err = %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths = %#v", paths)
	}
	if paths[0] != "DELETE /v1/namespaces/default/services/organization/instances/inst_ok" {
		t.Fatalf("first path = %s", paths[0])
	}
}

func stubRuntime(t *testing.T, goosName, goarchName string) func() {
	t.Helper()
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS = func() string { return goosName }
	currentGOARCH = func() string { return goarchName }
	return func() {
		currentGOOS = prevOS
		currentGOARCH = prevArch
	}
}
