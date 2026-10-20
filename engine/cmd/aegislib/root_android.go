//go:build android

package aegislib

// androidRoot returns the app-private storage root on Android. The
// net.aegisvpn app owns this directory; profile caches and the keychain
// live under it.
func androidRoot() string {
	return "/data/data/net.aegisvpn/files"
}
