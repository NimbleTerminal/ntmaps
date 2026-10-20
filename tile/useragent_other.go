//go:build !js

package tile

import "net/http"

func setUserAgent(req *http.Request, ua string) {
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
}
