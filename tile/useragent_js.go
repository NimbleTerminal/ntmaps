//go:build js

package tile

import "net/http"

// A script-set User-Agent triggers a CORS preflight that tile servers
// reject, and the browser sends its own anyway.
func setUserAgent(req *http.Request, ua string) {}
