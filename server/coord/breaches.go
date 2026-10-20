package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// handleBreaches proxies GET /v1/breaches/{email} to the HaveIBeenPwned v3
// API when HIBP_API_KEY is configured on the coordination server. Without a
// key the endpoint answers 501 with a clear message (per task contract) so
// clients can degrade gracefully instead of guessing.
//
// HIBP account lookups require a paid subscription key; passing the key
// through clients would leak it, which is why the proxy lives here.
func (s *server) handleBreaches(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimPrefix(r.URL.Path, "/v1/breaches/")
	if email == "" {
		writeErr(w, http.StatusBadRequest, "usage: GET /v1/breaches/{email}")
		return
	}
	email, err := url.QueryUnescape(email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "url-encode the email address")
		return
	}
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " ") {
		writeErr(w, http.StatusBadRequest, "not a valid email address")
		return
	}

	if s.hibpKey == "" {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"error": "breach lookup is not configured: set HIBP_API_KEY on the coordination server to enable the HaveIBeenPwned passthrough",
			"breaches": []any{},
		})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet,
		"https://haveibeenpwned.com/api/v3/breachedaccount/"+url.PathEscape(email), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("hibp-api-version", "3")
	req.Header.Set("Authorization", "Bearer "+s.hibpKey)
	req.Header.Set("User-Agent", "AegisVPN-Coord")

	resp, err := s.hibpClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "upstream breach service unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(http.MaxBytesReader(w, resp.Body, 4<<20))
		if err != nil {
			writeErr(w, http.StatusBadGateway, "upstream breach service returned a malformed body")
			return
		}
		var breaches json.RawMessage
		if json.Unmarshal(body, &breaches) != nil {
			writeErr(w, http.StatusBadGateway, "upstream breach service returned invalid JSON")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"breaches": breaches})
	case http.StatusNotFound:
		// HIBP semantics: 404 == no breaches for this account.
		writeJSON(w, http.StatusOK, map[string]any{"breaches": []any{}})
	case http.StatusUnauthorized, http.StatusForbidden:
		writeErr(w, http.StatusBadGateway, "upstream breach service rejected the configured HIBP_API_KEY")
	case http.StatusTooManyRequests:
		writeErr(w, http.StatusTooManyRequests, "upstream breach service rate limit hit, retry later")
	default:
		writeErr(w, http.StatusBadGateway, "upstream breach service returned status "+resp.Status)
	}
}
