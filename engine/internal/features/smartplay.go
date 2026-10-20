package features

import "github.com/shadow7019/AegisVPN/engine/internal/dns"

// SmartPlay overrides DNS resolution for streaming domains so that
// region-locked catalogues resolve through the provider's SmartPlay/DNS
// relays (which terminate inside the selected region) instead of the default
// tunnel DNS.
//
// SmartPlayDNSIPs are the provider relay resolvers. They are constants of
// this build; deployments override them by recompiling or via registry
// node records of type "dedicated".
var SmartPlayDNSIPs = []string{
	"198.18.0.11", // SmartPlay relay 1 (RFC 2544 benchmark range reserved for Aegis internal use)
	"198.18.0.12", // SmartPlay relay 2
}

// streamingSuffixes are the streaming provider domains SmartPlay covers.
var streamingSuffixes = []string{
	"netflix.com",
	"nflxvideo.net",
	"nflximg.net",
	"nflxext.com",
	"nflxso.net",
	"nflxsearch.net",
	"hulu.com",
	"hulustream.com",
	"max.com",
	"hbomax.com",
	"hbonow.com",
	"hbogo.com",
	"disneyplus.com",
	"disney-plus.net",
	"dslive.tv",
	"primevideo.com",
	"amazonvideo.com",
	"pv-cdn.net",
	"apple.tv",
	"tv.apple.com",
	"paramountplus.com",
	"cbsi.com",
	"peacocktv.com",
	"discoveryplus.com",
	"spotify.com",
	"scdn.co",
	"youtube.com",
	"googlevideo.com",
	"ytimg.com",
	"bbc.co.uk",
	"bbci.co.uk",
	"bbcstatic.net",
	"crunchyroll.com",
	"funimation.com",
	"sky.com",
	"nowtv.com",
	"dstv.com",
	"hotstar.com",
	"viki.com",
	"pluto.tv",
	"twitch.tv",
	"roku.com",
}

// SmartPlayRules renders the SmartPlay domain→DNS overrides for the DNS
// forwarder. Call dns.Server.SetRules with the result.
func SmartPlayRules() []dns.Rule {
	rules := make([]dns.Rule, 0, len(streamingSuffixes))
	for _, s := range streamingSuffixes {
		rules = append(rules, dns.Rule{Suffix: s, Servers: SmartPlayDNSIPs})
	}
	return rules
}

// IsSmartPlayDomain reports whether a domain is covered by SmartPlay.
func IsSmartPlayDomain(domain string) bool {
	for _, s := range streamingSuffixes {
		if domain == s || len(domain) > len(s) && domain[len(domain)-len(s)-1] == '.' && domain[len(domain)-len(s):] == s {
			return true
		}
	}
	return false
}
