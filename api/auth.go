package api

import (
	"crypto/subtle"
	"net"
	"net/http"
)

// mayStart is who starts a run, or it answers 401 / 403 and returns false. With keys set: a key, from anywhere.
// Without: only from this host (the script next to the service) -- reading goes through the proxy, starting a run
// does not. The peer address is the connection's own, never X-Forwarded-For: a header would let anyone claim to
// be local.
func (h *Handlers) mayStart(w http.ResponseWriter, r *http.Request) (string, bool) {
	if len(h.APIKeys) > 0 {
		key := r.Header.Get("X-API-Key")
		for name, k := range h.APIKeys {
			if key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
				return "key:" + name, true
			}
		}
		fail(w, http.StatusUnauthorized, "X-API-Key required")
		return "", false
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return "local", true
		}
	}
	fail(w, http.StatusForbidden, "runs are started from the service's own host")
	return "", false
}
