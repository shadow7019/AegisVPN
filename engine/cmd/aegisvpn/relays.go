// relays subcommand — browse and connect to free public relays directly.
//
//      aegisvpn relays list    [--country JP] [--json] [--force]
//      aegisvpn relays best    [--country JP]
//      aegisvpn relays connect [hostname | --country JP]
//      aegisvpn relays disconnect
//      aegisvpn relays status
package main

import (
        "context"
        "encoding/json"
        "errors"
        "flag"
        "fmt"
        "os"
        "path/filepath"
        "time"

        "github.com/shadow7019/AegisVPN/engine/publicrelays"
        "github.com/shadow7019/AegisVPN/engine/openvpn"
)

const handshakeWait = 30 * time.Second

func relayDir(root string) string { return filepath.Join(root, "relays") }

func sessionPath(root string) string { return filepath.Join(relayDir(root), "session.json") }

func runRelays(args []string, root string) {
        if len(args) == 0 {
                relaysUsage()
        }
        sub := args[0]
        rest := args[1:]

        fs := flag.NewFlagSet("relays "+sub, flag.ExitOnError)
        var country, rootFlag string
        var jsonOut, force bool
        fs.StringVar(&country, "country", "", "filter by ISO country code (e.g. JP)")
        fs.StringVar(&rootFlag, "root", root, "application data root")
        fs.BoolVar(&jsonOut, "json", false, "machine-readable output")
        fs.BoolVar(&force, "force", false, "bypass the 60s index cache")
        _ = fs.Parse(rest)
        root = rootFlag
        positional := fs.Args()

        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        switch sub {
        case "list":
                relaysRelayList(ctx, country, jsonOut, force)
        case "best":
                relaysBest(ctx, country)
        case "connect":
                host := ""
                if len(positional) > 0 {
                        host = positional[0]
                }
                os.Exit(relaysConnect(ctx, root, host, country))
        case "disconnect":
                relaysDisconnect(root)
        case "status":
                relaysStatus(root)
        default:
                relaysUsage()
        }
}

func relaysUsage() {
        fmt.Fprint(os.Stderr, `usage: aegisvpn relays <command> [flags]

commands:
  list       [--country XX] [--json] [--force]   browse the free relay index
  best       [--country XX]                      top-scored online relay
  connect    [hostname | --country XX]           one-click connect (OpenVPN)
  disconnect                                     stop the relay session
  status                                         current relay session state
`)
        os.Exit(2)
}

func fetchRelays(ctx context.Context, force bool) []publicrelays.Relay {
        relays, err := publicrelays.List(ctx, force)
        if err != nil {
                fatal("relays: %v", err)
        }
        return relays
}

func filterCountry(relays []publicrelays.Relay, cc string) []publicrelays.Relay {
        if cc == "" {
                return relays
        }
        var out []publicrelays.Relay
        for _, r := range relays {
                if equalFold(r.CountryCode, cc) {
                        out = append(out, r)
                }
        }
        return out
}

func equalFold(a, b string) bool {
        if len(a) != len(b) {
                return false
        }
        for i := 0; i < len(a); i++ {
                ca, cb := a[i], b[i]
                if 'A' <= ca && ca <= 'Z' {
                        ca += 'a' - 'A'
                }
                if 'A' <= cb && cb <= 'Z' {
                        cb += 'a' - 'A'
                }
                if ca != cb {
                        return false
                }
        }
        return true
}

func relaysRelayList(ctx context.Context, country string, jsonOut, force bool) {
        relays := fetchRelays(ctx, force)
        relays = filterCountry(relays, country)
        publicrelays.SortByScore(relays)
        if jsonOut {
                enc := json.NewEncoder(os.Stdout)
                enc.SetIndent("", "  ")
                if err := enc.Encode(relays); err != nil {
                        fatal("encode: %v", err)
                }
                return
        }
        fmt.Printf("%-4s %-28s %-24s %7s %10s  %s\n", "CC", "HOSTNAME", "ENDPOINT", "PING", "SPEED", "ONLINE")
        for i, r := range relays {
                if i >= 40 {
                        fmt.Printf("… and %d more (use --country to narrow, --json for everything)\n", len(relays)-40)
                        break
                }
                fmt.Printf("%-4s %-28s %-24s %7d %8d Mb/s  %t\n",
                        r.CountryCode, r.Hostname,
                        fmt.Sprintf("%s:%d/%s", r.IP, r.Port, r.Proto),
                        r.Ping, r.Speed/1_000_000, r.Online)
        }
}

func relaysBest(ctx context.Context, country string) {
        r, ok := publicrelays.Best(fetchRelays(ctx, false), country)
        if !ok {
                fatal("relays: no online relay matches country %q", country)
        }
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        if err := enc.Encode(r); err != nil {
                fatal("encode: %v", err)
        }
}

func relaysConnect(ctx context.Context, root, host, country string) int {
        relays := fetchRelays(ctx, false)

        if host == "" {
                r, ok := publicrelays.Best(relays, country)
                if !ok {
                        fmt.Fprintf(os.Stderr, "aegisvpn: no online relay matches country %q\n", country)
                        return 1
                }
                host = r.Hostname
        }

        // resolve endpoint metadata when the host is in the index
        endpoint := host
        for _, r := range relays {
                if equalFold(r.Hostname, host) {
                        endpoint = fmt.Sprintf("%s:%d/%s", r.IP, r.Port, r.Proto)
                        break
                }
        }

        fmt.Printf("resolving profile for %s …\n", host)
        cfgBytes, err := publicrelays.Profile(ctx, host)
        if err != nil {
                fmt.Fprintf(os.Stderr, "aegisvpn: %v\n", err)
                return 1
        }
        dir := relayDir(root)
        if err := os.MkdirAll(dir, 0o700); err != nil {
                fmt.Fprintf(os.Stderr, "aegisvpn: %v\n", err)
                return 1
        }
        cfgPath := filepath.Join(dir, safeName(host)+".ovpn")
        if err := os.WriteFile(cfgPath, cfgBytes, 0o600); err != nil {
                fmt.Fprintf(os.Stderr, "aegisvpn: %v\n", err)
                return 1
        }

        mgr := &openvpn.Manager{}
        info, err := mgr.Start(cfgPath, dir, host, endpoint)
        if err != nil {
                fmt.Fprintf(os.Stderr, "aegisvpn: %v\n", err)
                return 1
        }
        if err := openvpn.SaveSession(sessionPath(root), info); err != nil {
                fmt.Fprintf(os.Stderr, "aegisvpn: session tracking: %v\n", err)
        }

        fmt.Printf("openvpn pid %d starting (%s) …\n", info.PID, info.Binary)
        if openvpn.WaitForHandshake(info.LogPath, handshakeWait) {
                fmt.Printf("connected to %s (%s)\nlog: %s\n", host, endpoint, info.LogPath)
        } else {
                fmt.Fprintf(os.Stderr, "handshake not confirmed within %s — check %s\n", handshakeWait, info.LogPath)
                return 1
        }
        return 0
}

func relaysDisconnect(root string) {
        info, err := openvpn.KillPersisted(sessionPath(root))
        if err != nil {
                if errors.Is(err, os.ErrNotExist) {
                        fmt.Println("no relay session")
                        return
                }
                fatal("disconnect: %v", err)
        }
        fmt.Printf("disconnected from %s (pid %d)\n", info.Hostname, info.PID)
}

func relaysStatus(root string) {
        info, err := openvpn.LoadSession(sessionPath(root))
        if err != nil {
                out, _ := json.Marshal(map[string]any{"connected": false})
                fmt.Println(string(out))
                return
        }
        alive := openvpn.PIDAlive(info.PID)
        tail := tailFile(info.LogPath, 5)
        out, _ := json.MarshalIndent(map[string]any{
                "connected": alive,
                "session":   info,
                "log_tail":  tail,
        }, "", "  ")
        fmt.Println(string(out))
}

func safeName(s string) string {
        out := make([]rune, 0, len(s))
        for _, r := range s {
                switch {
                case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
                        out = append(out, r)
                default:
                        out = append(out, '-')
                }
        }
        return string(out)
}

func tailFile(path string, n int) []string {
        b, err := os.ReadFile(path)
        if err != nil {
                return nil
        }
        var lines []string
        start := 0
        for i := 0; i < len(b); i++ {
                if b[i] == '\n' {
                        lines = append(lines, string(b[start:i]))
                        start = i + 1
                }
        }
        if start < len(b) {
                lines = append(lines, string(b[start:]))
        }
        if len(lines) > n {
                lines = lines[len(lines)-n:]
        }
        return lines
}
