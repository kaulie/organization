// Command server runs the organization management HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

	service := org.NewService(org.NewMemoryStore())
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
