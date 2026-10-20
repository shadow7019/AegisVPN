// Package config implements the AegisVPN wg-quick profile parser.
//
// Profiles are standard wg-quick .conf files (see docs/PROTOCOL.md section 3)
// with two AegisVPN extensions in the [Interface] section:
//
//      AegisObfs    = true|false          (UDP obfuscation layer)
//      AegisObfsKey = <passphrase>        (obfuscation passphrase)
//
// The parser also produces WireGuard UAPI ("set") configuration strings that
// can be fed directly into device.Device.IpcSet. UAPI encodes Curve25519 keys
// as 64-char lowercase hex, so base64 keys from the profile are converted.
package config

import (
        "crypto/rand"
        "encoding/base64"
        "encoding/hex"
        "fmt"
        "net"
        "os"
        "strconv"
        "strings"

        "golang.org/x/crypto/curve25519"
)

// pubFromPriv derives the Curve25519 public key for a private key.
func pubFromPriv(priv []byte) ([]byte, error) {
        return curve25519.X25519(priv, curve25519.Basepoint)
}

// Peer is one [Peer] section of a wg-quick profile.
type Peer struct {
        PublicKey           string   // base64
        PresharedKey        string   // base64, empty if none
        AllowedIPs          []string // CIDRs
        Endpoint            string   // host:port, empty if none
        PersistentKeepalive int      // seconds, 0 = disabled
}

// Profile is a parsed wg-quick profile.
type Profile struct {
        PrivateKey string   // base64
        Addresses  []string // CIDRs, e.g. "10.8.0.2/32"
        DNS        []string // DNS servers
        MTU        int      // 0 = auto
        ListenPort int
        FwMark     uint32

        // AegisVPN extensions.
        Obfs    bool
        ObfsKey string

        Peers []*Peer
}

func stripInlineComment(v string) string {
        if i := strings.IndexByte(v, '#'); i >= 0 {
                v = v[:i]
        }
        return strings.TrimSpace(v)
}

// Parse parses a wg-quick profile (with AegisVPN extensions) from text.
func Parse(text string) (*Profile, error) {
        p := &Profile{}
        var peer *Peer
        section := ""
        for ln, raw := range strings.Split(text, "\n") {
                line := strings.TrimSpace(raw)
                if line == "" || strings.HasPrefix(line, "#") {
                        continue
                }
                if strings.HasPrefix(line, "[") {
                        sec := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
                        sec = strings.TrimSpace(sec)
                        switch sec {
                        case "interface":
                                section = "interface"
                                continue
                        case "peer":
                                section = "peer"
                                peer = &Peer{}
                                p.Peers = append(p.Peers, peer)
                                continue
                        default:
                                return nil, fmt.Errorf("line %d: unknown section [%s]", ln+1, sec)
                        }
                }
                eq := strings.Index(line, "=")
                if eq < 0 {
                        return nil, fmt.Errorf("line %d: expected key = value", ln+1)
                }
                key := strings.ToLower(strings.TrimSpace(line[:eq]))
                val := stripInlineComment(line[eq+1:])
                if val == "" && key != "dns" && key != "address" && key != "allowedips" {
                        continue
                }
                var err error
                if section == "interface" {
                        switch key {
                        case "privatekey":
                                p.PrivateKey, err = normKey(val)
                        case "listenport":
                                p.ListenPort, err = atoi(val)
                        case "fwmark":
                                v := strings.TrimPrefix(val, "0x")
                                var mark uint64
                                mark, err = strconv.ParseUint(v, 0, 32)
                                p.FwMark = uint32(mark)
                        case "mtu":
                                p.MTU, err = atoi(val)
                        case "address":
                                p.Addresses = append(p.Addresses, splitList(val)...)
                        case "dns":
                                p.DNS = append(p.DNS, splitList(val)...)
                        case "aegisobfs":
                                p.Obfs, err = parseBool(val)
                        case "aegisobfskey":
                                p.ObfsKey = val
                        case "table", "postup", "preup", "postdown", "predown", "saveconfig":
                                // Accepted wg-quick keys; engine manages routing itself.
                        default:
                                return nil, fmt.Errorf("line %d: unknown [Interface] key %q", ln+1, key)
                        }
                } else if section == "peer" {
                        switch key {
                        case "publickey":
                                peer.PublicKey, err = normKey(val)
                        case "presharedkey":
                                peer.PresharedKey, err = normKey(val)
                        case "allowedips":
                                peer.AllowedIPs = append(peer.AllowedIPs, splitList(val)...)
                        case "endpoint":
                                peer.Endpoint = val
                        case "persistentkeepalive":
                                peer.PersistentKeepalive, err = atoi(val)
                        default:
                                return nil, fmt.Errorf("line %d: unknown [Peer] key %q", ln+1, key)
                        }
                } else {
                        return nil, fmt.Errorf("line %d: key outside of any section", ln+1)
                }
                if err != nil {
                        return nil, fmt.Errorf("line %d: %w", ln+1, err)
                }
        }
        if p.PrivateKey == "" {
                return nil, fmt.Errorf("profile: missing PrivateKey in [Interface]")
        }
        if len(p.Peers) == 0 {
                return nil, fmt.Errorf("profile: no [Peer] section found")
        }
        for i, peer := range p.Peers {
                if peer.PublicKey == "" {
                        return nil, fmt.Errorf("profile: [Peer] #%d missing PublicKey", i+1)
                }
        }
        return p, nil
}

// LoadFile reads and parses a profile from disk.
func LoadFile(path string) (*Profile, error) {
        b, err := os.ReadFile(path)
        if err != nil {
                return nil, fmt.Errorf("load profile %s: %w", path, err)
        }
        return Parse(string(b))
}

// splitList splits comma- and whitespace-separated wg-quick value lists.
func splitList(v string) []string {
        fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
        out := make([]string, 0, len(fields))
        for _, f := range fields {
                if f = strings.TrimSpace(f); f != "" {
                        out = append(out, f)
                }
        }
        return out
}

// normKey validates a base64-encoded Curve25519 key and returns it unchanged.
func normKey(v string) (string, error) {
        raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
        if err != nil {
                return "", fmt.Errorf("invalid base64 key: %w", err)
        }
        if len(raw) != 32 {
                return "", fmt.Errorf("key must decode to 32 bytes, got %d", len(raw))
        }
        return strings.TrimSpace(v), nil
}

func atoi(v string) (int, error) {
        n, err := strconv.Atoi(strings.TrimSpace(v))
        if err != nil {
                return 0, fmt.Errorf("invalid integer %q", v)
        }
        return n, nil
}

func parseBool(v string) (bool, error) {
        switch strings.ToLower(strings.TrimSpace(v)) {
        case "true", "yes", "on", "1":
                return true, nil
        case "false", "no", "off", "0":
                return false, nil
        }
        return false, fmt.Errorf("invalid boolean %q", v)
}

// b64ToHex converts a base64 WireGuard key to the hex form used by UAPI.
func b64ToHex(b64 string) (string, error) {
        raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
        if err != nil {
                return "", err
        }
        return hex.EncodeToString(raw), nil
}

// IpcConfig renders the profile as a WireGuard UAPI "set" configuration
// string suitable for device.Device.IpcSet. If fwmark > 0 a fwmark= line is
// emitted (the bind must support SetMark, e.g. conn.DefaultBind on Linux).
//
// See https://git.zx2c4.com/wireguard-tools/about/src/man/wg.8 — the
// userspace IPC protocol section.
func (p *Profile) IpcConfig(fwmark uint32) (string, error) {
        priv, err := b64ToHex(p.PrivateKey)
        if err != nil {
                return "", fmt.Errorf("profile private key: %w", err)
        }
        var b strings.Builder
        fmt.Fprintf(&b, "private_key=%s\n", priv)
        if p.ListenPort > 0 {
                fmt.Fprintf(&b, "listen_port=%d\n", p.ListenPort)
        }
        if fwmark > 0 {
                fmt.Fprintf(&b, "fwmark=%d\n", fwmark)
        }
        b.WriteString("replace_peers=true\n")
        for i, peer := range p.Peers {
                pub, err := b64ToHex(peer.PublicKey)
                if err != nil {
                        return "", fmt.Errorf("peer #%d public key: %w", i+1, err)
                }
                fmt.Fprintf(&b, "public_key=%s\n", pub)
                if peer.PresharedKey != "" {
                        psk, err := b64ToHex(peer.PresharedKey)
                        if err != nil {
                                return "", fmt.Errorf("peer #%d preshared key: %w", i+1, err)
                        }
                        fmt.Fprintf(&b, "preshared_key=%s\n", psk)
                }
                if peer.Endpoint != "" {
                        fmt.Fprintf(&b, "endpoint=%s\n", peer.Endpoint)
                }
                if peer.PersistentKeepalive > 0 {
                        fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", peer.PersistentKeepalive)
                }
                for _, ip := range peer.AllowedIPs {
                        fmt.Fprintf(&b, "allowed_ip=%s\n", ip)
                }
        }
        return b.String(), nil
}

// AllowedIPs returns the union of all peers' AllowedIPs.
func (p *Profile) AllowedIPs() []string {
        var out []string
        seen := map[string]bool{}
        for _, peer := range p.Peers {
                for _, ip := range peer.AllowedIPs {
                        if !seen[ip] {
                                seen[ip] = true
                                out = append(out, ip)
                        }
                }
        }
        return out
}

// EndpointHosts returns the endpoint IPs of all peers that have one set.
// DNS names are resolved; failures for individual peers are skipped.
func (p *Profile) EndpointHosts() []net.IP {
        var out []net.IP
        for _, peer := range p.Peers {
                if peer.Endpoint == "" {
                        continue
                }
                host, _, err := net.SplitHostPort(peer.Endpoint)
                if err != nil {
                        continue
                }
                if ip := net.ParseIP(host); ip != nil {
                        out = append(out, ip)
                        continue
                }
                if addrs, err := net.LookupIP(host); err == nil {
                        out = append(out, addrs...)
                }
        }
        return out
}

// GenerateKey creates a fresh Curve25519 keypair and returns the private and
// public keys base64-encoded (used by the CLI/registry to provision clients).
func GenerateKey() (privB64, pubB64 string, err error) {
        priv := make([]byte, 32)
        if _, err = rand.Read(priv); err != nil {
                return "", "", err
        }
        priv[0] &= 248
        priv[31] &= 127
        priv[31] |= 64
        pub, err := pubFromPriv(priv)
        if err != nil {
                return "", "", err
        }
        return base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub), nil
}

// PublicFromPrivate derives the base64-encoded WireGuard public key
// corresponding to a base64-encoded Curve25519 private key.
func PublicFromPrivate(privB64 string) (string, error) {
        raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privB64))
        if err != nil {
                return "", fmt.Errorf("invalid base64 key: %w", err)
        }
        if len(raw) != 32 {
                return "", fmt.Errorf("key must decode to 32 bytes, got %d", len(raw))
        }
        pub, err := pubFromPriv(raw)
        if err != nil {
                return "", err
        }
        return base64.StdEncoding.EncodeToString(pub), nil
}
