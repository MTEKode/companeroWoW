package mcpserver

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// sessionToken is the bearer token that every client of the MCP endpoint must send. It is
// drawn once per process, so the CLIs that the app launches receive it in their configuration
// and a token from an earlier run is useless. Other local programs cannot read it.
var sessionToken = newToken()

// Token returns the bearer token of this process. The CLI providers send it to the endpoint.
func Token() string { return sessionToken }

func newToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("mcpserver: no pude generar el token: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// requireToken rejects requests that do not carry "Authorization: Bearer <token>". Any other
// local process can reach 127.0.0.1, so the port alone does not identify the caller.
func requireToken(next http.Handler) http.Handler {
	want := []byte("Bearer " + sessionToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="companero"`)
			http.Error(w, "no autorizado", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
