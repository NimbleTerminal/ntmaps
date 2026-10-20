//go:build js

package httpua

import "net/http"

// Set is a no-op in the browser: a script-set User-Agent triggers a CORS
// preflight that map services reject, and the browser sends its own.
func Set(req *http.Request, ua string) {}
