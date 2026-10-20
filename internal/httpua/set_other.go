//go:build !js

package httpua

import "net/http"

// Set puts ua, or Default when ua is empty, on the request.
func Set(req *http.Request, ua string) {
	if ua == "" {
		ua = Default()
	}
	req.Header.Set("User-Agent", ua)
}
