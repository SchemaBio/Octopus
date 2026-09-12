package middleware

import (
	"net/http"
	"strings"
	"time"
)

// TimeoutExceptLongRunning applies a whole-request timeout to JSON API routes
// while leaving upload and download paths unbounded at the handler layer.
// Connection-level Slowloris protection remains ReadHeaderTimeout.
func TimeoutExceptLongRunning(next http.Handler, d time.Duration) http.Handler {
	if next == nil || d <= 0 {
		return next
	}
	timed := http.TimeoutHandler(next, d, `{"error":"request timeout"}`)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r != nil && SkipRequestTimeout(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		timed.ServeHTTP(w, r)
	})
}

func SkipRequestTimeout(path string) bool {
	p := strings.ToLower(path)
	for _, marker := range []string{
		"/upload",
		"/export",
		"/result-package",
		"/archive",
		"/data",
		"/reports/",
	} {
		if strings.Contains(p, marker) {
			return true
		}
	}
	return false
}
