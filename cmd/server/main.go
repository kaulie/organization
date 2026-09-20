// Command server runs the organization management HTTP service.
//
// General API Info for swaggo/swag — this is the annotation entry point that
// the release/CI step turns into the service contract (swag init -g
// cmd/server/main.go), which is then registered in the service registry.
// Annotations are the single source of truth: change an endpoint, change its
// annotation next to the handler, and the next release refreshes the contract.
//
// Runtime has zero dependency on them (no swaggo import anywhere). @version is
// only a human-readable fallback: the registration step passes the release's
// APP_VERSION explicitly, so the registry always sees the deployed hash.
//
// @title           organization
// @version         1.0.0
// @description     组织架构管理服务：部门维护（新增 / 重命名 / 详情）与人员（HUMAN / AGENT）注册与查询。
// @BasePath        /
// @schemes         http
// @host            127.0.0.1:4244
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kaulie/organization/internal/httpapi"
	"github.com/kaulie/organization/internal/org"
)

// version is the build version. The release build injects the deployment hash
// via -ldflags "-X main.version=<hash>"; at runtime APP_VERSION (injected by
// the deployment control plane) wins, which keeps a manually started process
// consistent with the deployed package.
var version = "dev"

// defaultListenPort is used when neither ORG_ADDR nor SERVICE_PORT is set.
const defaultListenPort = "8080"

const (
	// defaultDataDir is the store location used when ORG_DATA_DIR is unset. It
	// mirrors the deployment layout: scripts/start.sh exports ORG_DATA_DIR
	// (…/backend/data) and the deployment tool keeps backend/data/ across
	// releases, which is what makes data survive a redeploy.
	defaultDataDir = "backend/data"
	// storeFileName is the JSON snapshot kept inside the data directory.
	storeFileName = "org-store.json"
)

// dataFilePath resolves the JSON snapshot backing the store.
func dataFilePath() string {
	dir := strings.TrimSpace(os.Getenv("ORG_DATA_DIR"))
	if dir == "" {
		dir = defaultDataDir
	}
	return filepath.Join(dir, storeFileName)
}

// resolveVersion prefers the APP_VERSION injected by the deployment platform.
func resolveVersion() string {
	if v := strings.TrimSpace(os.Getenv("APP_VERSION")); v != "" {
		return v
	}
	return version
}

// listenAddr resolves the listen address from the environment.
//
// Precedence: ORG_ADDR > SERVICE_PORT > defaultListenPort. Setting only
// SERVICE_PORT listens on all interfaces; ORG_ADDR also pins the interface.
//
// The port variable is deliberately named SERVICE_PORT instead of the generic
// PORT: PORT is routinely exported by unrelated runtimes sharing the host
// (e.g. a co-located web app), which would silently relocate this listener.
// scripts/start.sh bridges the port injected by the deployment control plane
// (it injects PORT, per the platform contract) into ORG_ADDR/SERVICE_PORT.
func listenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("ORG_ADDR")); addr != "" {
		return addr
	}
	port := strings.TrimSpace(os.Getenv("SERVICE_PORT"))
	if port == "" {
		port = defaultListenPort
	}
	return ":" + port
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	appVersion := resolveVersion()
	addr := listenAddr()
	dataFile := dataFilePath()

	// The store is persisted: the service is deployed by restarting the
	// process, so an in-memory-only store would lose everything on every
	// release. A data file that exists but cannot be decoded is fatal on
	// purpose — never silently start empty on top of existing data.
	store, err := org.NewFileStore(dataFile)
	if err != nil {
		logger.Error("store_open_failed", "dataFile", dataFile, "err", err)
		os.Exit(1)
	}
	service := org.NewService(store)
	// Log the loaded size: after a redeploy this line distinguishes "data was
	// restored" from "the service started empty".
	logger.Info("store_loaded",
		"dataFile", dataFile,
		"departments", len(store.ListDepartments()),
		"persons", len(store.ListPersons("")),
	)

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(service, logger, httpapi.WithVersion(appVersion)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("server_starting", "addr", addr, "version", appVersion)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("server_shutting_down", "version", appVersion)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server_shutdown_error", "err", err)
		os.Exit(1)
	}
	logger.Info("server_stopped")
}
