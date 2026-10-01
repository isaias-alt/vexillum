package forum

import (
	"crypto/subtle"
	"net/http"
)

// What a browser can send to /api/s/{key}/... lands in the agent's inbox as
// the user's own instructions, so those routes are the sensitive surface of
// the whole server (forum-tool guards its equivalent POST /api/:key/prompts
// the same way). A session key is the hash of a file path - guessable, not
// a secret - so knowing it must never be enough. Three independent checks
// stand between a request and the queue:
//
//  1. the Host is loopback (ServeHTTP) - defeats DNS rebinding;
//  2. the request is same-origin (sameOrigin) - defeats a web page the user
//     happens to have open, which can fire cross-origin requests at
//     127.0.0.1 but cannot forge the Origin or Sec-Fetch-Site headers;
//  3. the request carries the session's token (X-Forum-Token) - a secret
//     that only the session's own pages are served, so a cross-origin page
//     cannot read it either.

// tokenHeader carries the session token on every browser API request.
const tokenHeader = "X-Forum-Token"

// sameOrigin reports whether r demonstrably comes from a page served by this
// server. A mutating request must prove it with an Origin header equal to
// its own Host (every browser sends one on a same-origin POST/PUT/DELETE) or
// Sec-Fetch-Site: same-origin; a read may also come with neither (a
// non-browser client, which still needs the token).
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == "http://"+r.Host
	}
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	return r.Method == http.MethodGet || r.Method == http.MethodHead
}

// browserAPI gates a handler behind the same-origin and session-token checks
// for the {key} session in its path.
func (s *Server) browserAPI(next func(w http.ResponseWriter, r *http.Request, key string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !ValidSessionKey(key) {
			writeError(w, http.StatusNotFound, "no_session", "no such forum session")
			return
		}
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
			return
		}
		want, err := s.hub.Token(key)
		if err != nil {
			writeHubError(w, err)
			return
		}
		got := r.Header.Get(tokenHeader)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid session token")
			return
		}
		next(w, r, key)
	}
}
