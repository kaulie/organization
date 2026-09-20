package httpapi

import (
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// TestRoutesMatchAnnotations keeps the two halves of the service contract in
// sync: the routes the mux really serves (routes()) and the swag annotations
// the release step turns into the registered OpenAPI document.
//
// The failure modes it prevents are asymmetric. A route without an annotation
// silently disappears from the service registry (consumers never learn it
// exists); an annotation without a route advertises an endpoint that answers
// 404 — the worse of the two, because it lies to callers and to the panel.
func TestRoutesMatchAnnotations(t *testing.T) {
	annotated := annotatedRoutes(t)

	h := &Handler{}
	served := make([]string, 0, len(annotated))
	for _, rt := range h.routes() {
		served = append(served, rt.method+" "+rt.path)
	}

	sort.Strings(annotated)
	sort.Strings(served)
	if got, want := strings.Join(annotated, "\n"), strings.Join(served, "\n"); got != want {
		t.Errorf("swag 注解与路由表不一致（注解是契约的唯一真源，两边必须一一对应）\n注解 %d 条:\n%s\n路由 %d 条:\n%s",
			len(annotated), got, len(served), want)
	}
}

// annotatedRoutes parses the package sources and collects every @Router
// annotation as "METHOD /path". It deliberately parses comments instead of
// importing swag: the annotation is the source of truth, so the check must work
// with nothing but the standard library and the source files.
func annotatedRoutes(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析包目录失败: %v", err)
	}

	var routes []string
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, group := range file.Comments {
				for _, comment := range group.List {
					line := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
					if !strings.HasPrefix(line, "@Router") {
						continue
					}
					// 期望格式：@Router /api/v1/persons/{id} [get]
					fields := strings.Fields(line)
					if len(fields) != 3 {
						t.Errorf("%s: 无法解析注解 %q（期望 @Router <path> [method]）", name, comment.Text)
						continue
					}
					routes = append(routes, strings.ToUpper(strings.Trim(fields[2], "[]"))+" "+fields[1])
				}
			}
		}
	}
	if len(routes) == 0 {
		t.Fatal("没有解析到任何 @Router 注解：注解没了，注册出去的契约就只剩 /health")
	}
	return routes
}
