package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

// webUIFS holds the embedded single-page UI.
//
// The UI is served from inside the binary on purpose: the deployment package
// ships only bin/orgd plus scripts/, so a UI that needed files next to the
// executable would break as soon as deploy.sh rsyncs the runtime directory.
//
//go:embed webui
var webUIFS embed.FS

// uiPrefix is the mount point of the UI assets.
const uiPrefix = "/ui/"

// uiIndexHTML is the page served at "/". It is read once at init: the content
// lives in the binary, so a request-time read cannot fail.
var uiIndexHTML = func() []byte {
	data, err := webUIFS.ReadFile("webui/index.html")
	if err != nil {
		panic("httpapi: embedded webui/index.html is missing: " + err.Error())
	}
	return data
}()

// mountUI registers the browser UI: "/" renders the single page, "/ui/*" serves
// its stylesheet and script.
func mountUI(mux *http.ServeMux) {
	assets, err := fs.Sub(webUIFS, "webui")
	if err != nil {
		panic("httpapi: embedded webui assets are unavailable: " + err.Error())
	}
	mux.HandleFunc("GET /{$}", serveUIIndex)
	// StripPrefix is required: the asset FS is rooted at webui/, while the
	// request path still carries the /ui/ mount point. Without it the lookup
	// becomes webui/ui/app.js and every asset request 404s.
	mux.Handle("GET "+uiPrefix, noStore(http.StripPrefix(uiPrefix, http.FileServerFS(assets))))
}

// serveUIIndex answers GET / with the single page.
func serveUIIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(uiIndexHTML)
}

// noStore marks UI assets as uncacheable. The UI is redeployed as a whole, so
// a cached index.html referencing a stale app.js would render a broken page.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
