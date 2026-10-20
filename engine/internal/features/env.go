package features

import "os"

// coordURL resolves the coordination server base URL: the AEGIS_COORD_URL
// environment variable, else the documented default.
func coordURL() string {
	if v := os.Getenv("AEGIS_COORD_URL"); v != "" {
		return v
	}
	return "https://coord.aegisvpn.net"
}
