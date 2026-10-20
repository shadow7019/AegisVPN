// Package features implements AegisVPN value-add services that ride on top
// of the engine: the Dark Web Monitor (HIBP breach checks), the Meshnet
// device directory and SmartPlay streaming DNS rules.
package features

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Breach is one entry of the HaveIBeenPwned breach report.
type Breach struct {
	Name        string   `json:"Name"`
	Title       string   `json:"Title"`
	Domain      string   `json:"Domain"`
	BreachDate  string   `json:"BreachDate"`
	AddedDate   string   `json:"AddedDate"`
	ModifiedDate string  `json:"ModifiedDate"`
	PwnCount    int64    `json:"PwnCount"`
	Description string   `json:"Description"`
	DataClasses []string `json:"DataClasses"`
	IsVerified  bool     `json:"IsVerified"`
	IsSensitive bool     `json:"IsSensitive"`
	IsActive    bool     `json:"IsActive"`
}

// HIBPClient queries the HaveIBeenPwned v3 API (Dark Web Monitor).
type HIBPClient struct {
	BaseURL string
	hc      *http.Client
}

// NewHIBPClient creates a HIBP v3 client.
func NewHIBPClient() *HIBPClient {
	return &HIBPClient{
		BaseURL: "https://haveibeenpwned.com/api/v3",
		hc:      &http.Client{Timeout: 20 * time.Second},
	}
}

// BreachedAccount returns the breaches for an email account.
// Per HIBP v3 semantics, HTTP 404 means "no breaches found" and is returned
// as an empty slice with a nil error.
func (h *HIBPClient) BreachedAccount(email string) ([]Breach, error) {
	endpoint := fmt.Sprintf("%s/breachedaccount/%s?truncateResponse=false",
		h.BaseURL, url.PathEscape(email))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// HIBP v3 requires a meaningful User-Agent; we identify the product.
	req.Header.Set("User-Agent", "AegisVPN")
	req.Header.Set("Accept", "application/json")
	resp, err := h.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hibp: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to decode
	case http.StatusNotFound:
		return []Breach{}, nil // no breaches
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("hibp: access denied (HTTP %d) — an api key may be required", resp.StatusCode)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("hibp: rate limited (HTTP 429), retry later")
	default:
		return nil, fmt.Errorf("hibp: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var breaches []Breach
	if err := json.Unmarshal(body, &breaches); err != nil {
		return nil, fmt.Errorf("hibp: decode response: %w", err)
	}
	return breaches, nil
}
