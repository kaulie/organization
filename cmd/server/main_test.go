package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The service contract is generated from the swag annotations in this file, so
// the General API Info block must survive refactors of cmd/server: swag reads
// its entry point (-g) exclusively for that block, and without it the generated
// document has no metadata at all (and the registration step keeps a stale or
// empty contract). Its absence is therefore a build-time bug, not a doc nit.
func TestMainCarriesGeneralAPIInfo(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, directive := range []string{"@title", "@version", "@BasePath"} {
		if !strings.Contains(text, directive) {
			t.Errorf("main.go 缺少 swag 注解 %s（General API Info 是生成契约的入口）", directive)
		}
	}
	// Must sit on the package comment: swag only parses the block above "package main".
	if !strings.Contains(text, "package main") {
		t.Fatal("main.go 里找不到 package main")
	}
	if strings.Index(text, "@title") > strings.Index(text, "package main") {
		t.Error("General API Info 必须在 package main 之前，否则 swag 读不到")
	}
}

// The deployment platform preserves backend/data/ across releases, and
// scripts/start.sh exports ORG_DATA_DIR pointing at it, which is what makes the
// stored data survive a redeploy.
func TestDataFilePathUsesOrgDataDir(t *testing.T) {
	t.Setenv("ORG_DATA_DIR", "/Users/example/runtime/organization/backend/data")
	want := filepath.Join("/Users/example/runtime/organization/backend/data", "org-store.json")
	if got := dataFilePath(); got != want {
		t.Errorf("dataFilePath() = %q, want %q", got, want)
	}
}

func TestDataFilePathFallsBackToBackendData(t *testing.T) {
	want := filepath.Join("backend", "data", "org-store.json")

	t.Setenv("ORG_DATA_DIR", "")
	if got := dataFilePath(); got != want {
		t.Errorf("dataFilePath() = %q, want %q", got, want)
	}

	t.Setenv("ORG_DATA_DIR", "   ")
	if got := dataFilePath(); got != want {
		t.Errorf("dataFilePath() with a blank value = %q, want %q", got, want)
	}
}

func TestListenAddrDefaultsWhenNothingSet(t *testing.T) {
	t.Setenv("ORG_ADDR", "")
	t.Setenv("SERVICE_PORT", "")
	if got := listenAddr(); got != ":8080" {
		t.Errorf("listenAddr() = %q, want :8080", got)
	}
}

func TestListenAddrUsesServicePort(t *testing.T) {
	t.Setenv("ORG_ADDR", "")
	t.Setenv("SERVICE_PORT", "9000")
	if got := listenAddr(); got != ":9000" {
		t.Errorf("listenAddr() = %q, want :9000", got)
	}
}

func TestListenAddrPrefersOrgAddrOverServicePort(t *testing.T) {
	t.Setenv("ORG_ADDR", "127.0.0.1:4250")
	t.Setenv("SERVICE_PORT", "9000")
	if got := listenAddr(); got != "127.0.0.1:4250" {
		t.Errorf("listenAddr() = %q, want 127.0.0.1:4250", got)
	}
}

// The generic PORT variable must never drive our listener: on a shared host it
// is commonly exported by an unrelated runtime (measured on this machine:
// PORT=4211 comes from the co-located web-cursor deployment), which would
// silently move this service onto someone else's port.
func TestListenAddrIgnoresGenericPort(t *testing.T) {
	t.Setenv("ORG_ADDR", "")
	t.Setenv("SERVICE_PORT", "")
	t.Setenv("PORT", "4211")
	if got := listenAddr(); got != ":8080" {
		t.Errorf("listenAddr() = %q, want :8080 (generic PORT must be ignored)", got)
	}
}

func TestListenAddrTrimsBlankValues(t *testing.T) {
	t.Setenv("ORG_ADDR", "   ")
	t.Setenv("SERVICE_PORT", "  ")
	if got := listenAddr(); got != ":8080" {
		t.Errorf("listenAddr() = %q, want :8080", got)
	}

	t.Setenv("SERVICE_PORT", " 9000 ")
	if got := listenAddr(); got != ":9000" {
		t.Errorf("listenAddr() = %q, want :9000", got)
	}
}

func TestResolveVersion(t *testing.T) {
	t.Setenv("APP_VERSION", "")
	if got := resolveVersion(); got != version {
		t.Errorf("resolveVersion() = %q, want baked version %q", got, version)
	}

	t.Setenv("APP_VERSION", "a1d7ee34")
	if got := resolveVersion(); got != "a1d7ee34" {
		t.Errorf("resolveVersion() = %q, want a1d7ee34", got)
	}

	t.Setenv("APP_VERSION", "   ")
	if got := resolveVersion(); got != version {
		t.Errorf("resolveVersion() = %q, want baked version %q for blank input", got, version)
	}
}
