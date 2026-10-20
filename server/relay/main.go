// Command relay is the AegisVPN Double VPN relay.
//
// It runs TWO real wireguard-go devices (golang.zx2c4.com/wireguard/{device,conn,tun})
// and bridges them with a userspace IPv4/IPv6 router:
//
//	WireGuard client                      exit node (10.8.1.1)
//	 (10.8.0.2)                            WireGuard server
//	     |  encrypted WG over UDP               ^  encrypted WG over UDP
//	     v                                      |
//	+-------------------  relay  --------------------------+
//	| device A (entry)              device B (exit side)   |
//	| listen :51822                 connects to exit node  |
//	| peers: client peers           peer: exit node pubkey |
//	| tun: aegis0 (usTun)           tun: aegis1 (usTun)    |
//	|          \                        /                  |
//	|           userspace IPv4/IPv6 router                  |
//	|        10.8.0.0/24 -> A   10.8.1.0/24 -> B            |
//	|        default     -> B                               |
//	+-------------------------------------------------------+
//
// See README-relay.md for the full topology and key flow.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
)

type config struct {
	entryListenPort int
	entryPrivateKey []byte // 32 bytes, WireGuard private key
	exitPrivateKey  []byte // 32 bytes, relay's key on the inner (exit-side) WG link
	exitPubkey      []byte // 32 bytes, exit node public key
	exitEndpoint    string // ip:port, pre-resolved
	clients         []clientPeer
	subnetA         netip.Prefix
	subnetB         netip.Prefix
	entryAddr       netip.Addr
	innerAddr       netip.Addr
	mtu             int
	enableV6        bool
	statusListen    string
	verbose         bool
}

type clientPeer struct {
	pubkey []byte // 32 bytes
	ip     netip.Addr
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	cfg, err := parseConfig()
	if err != nil {
		log.Fatalf("relay: %v", err)
	}

	if err := run(cfg); err != nil {
		log.Fatalf("relay: %v", err)
	}
}

func parseConfig() (*config, error) {
	var (
		cfg         config
		entryKeyStr = flag.String("entry-private-key", envOr("AEGIS_ENTRY_PRIVATE_KEY", ""), "entry WireGuard private key (base64); generated when empty")
		exitKeyStr  = flag.String("exit-private-key", envOr("AEGIS_EXIT_PRIVATE_KEY", ""), "exit-side WireGuard private key (base64); generated when empty")
		exitPubStr  = flag.String("exit-pubkey", envOr("AEGIS_EXIT_PUBKEY", ""), "exit node WireGuard public key (base64) — required")
		exitEpStr   = flag.String("exit-endpoint", envOr("AEGIS_EXIT_ENDPOINT", "127.0.0.1:51821"), "exit node WireGuard endpoint (host:port)")
		clientsStr  = flag.String("clients", envOr("AEGIS_CLIENTS", ""), "client peers as comma-separated pubkey@10.8.0.N/32 (N>=2)")
		port        = flag.Int("entry-port", intEnvOr("AEGIS_ENTRY_PORT", 51822), "UDP port the entry WireGuard device listens on")
		subnetAStr  = flag.String("subnet-a", envOr("AEGIS_SUBNET_A", "10.8.0.0/24"), "client-facing subnet A")
		subnetBStr  = flag.String("subnet-b", envOr("AEGIS_SUBNET_B", "10.8.1.0/24"), "exit-side subnet B")
		entryAStr   = flag.String("entry-addr", envOr("AEGIS_ENTRY_ADDR", "10.8.0.1"), "relay address on subnet A")
		innerBStr   = flag.String("inner-addr", envOr("AEGIS_INNER_ADDR", "10.8.1.2"), "relay address on subnet B")
		mtu         = flag.Int("mtu", intEnvOr("AEGIS_MTU", 1420), "tunnel MTU for both devices")
		enableV6    = flag.Bool("enable-v6", envOr("AEGIS_ENABLE_V6", "true") == "true", "install an IPv6 default route to the exit side")
		status      = flag.String("status-listen", envOr("AEGIS_STATUS_LISTEN", ":8090"), "HTTP status endpoint (empty disables)")
		verbose     = flag.Bool("verbose", envOr("AEGIS_VERBOSE", "") == "true", "verbose WireGuard logging")
	)
	flag.Parse()

	cfg.entryListenPort = *port
	cfg.mtu = *mtu
	cfg.enableV6 = *enableV6
	cfg.statusListen = *status
	cfg.verbose = *verbose

	var err error
	if cfg.entryPrivateKey, err = keyFromFlag(*entryKeyStr, "entry-private-key"); err != nil {
		return nil, err
	}
	if cfg.entryPrivateKey == nil {
		cfg.entryPrivateKey = genPrivateKey()
		log.Printf("relay: generated fresh entry private key (public key %s) — persist it via AEGIS_ENTRY_PRIVATE_KEY",
			pubB64(cfg.entryPrivateKey))
	}
	if cfg.exitPrivateKey, err = keyFromFlag(*exitKeyStr, "exit-private-key"); err != nil {
		return nil, err
	}
	if cfg.exitPrivateKey == nil {
		cfg.exitPrivateKey = genPrivateKey()
		log.Printf("relay: generated fresh exit-side private key (public key %s) — persist it via AEGIS_EXIT_PRIVATE_KEY",
			pubB64(cfg.exitPrivateKey))
	}
	if cfg.exitPubkey, err = keyFromFlag(*exitPubStr, "exit-pubkey"); err != nil {
		return nil, err
	}
	if cfg.exitPubkey == nil {
		return nil, errors.New("exit node public key is required (--exit-pubkey / AEGIS_EXIT_PUBKEY)")
	}

	ep := *exitEpStr
	if host, portStr, splitErr := net.SplitHostPort(ep); splitErr == nil && net.ParseIP(host) == nil {
		// StdNetBind parses endpoints with netip.ParseAddrPort, which rejects
		// hostnames; resolve them here so configs like vpn.example.com work.
		ips, resolveErr := net.LookupIP(host)
		if resolveErr != nil || len(ips) == 0 {
			return nil, fmt.Errorf("resolve exit endpoint host %q: %w", host, resolveErr)
		}
		ep = net.JoinHostPort(ips[0].String(), portStr)
		log.Printf("relay: resolved exit endpoint %s -> %s", *exitEpStr, ep)
	}
	if _, _, splitErr := net.SplitHostPort(ep); splitErr != nil {
		return nil, fmt.Errorf("exit endpoint must be host:port, got %q", *exitEpStr)
	}
	cfg.exitEndpoint = ep

	for _, spec := range strings.Split(*clientsStr, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		cp, parseErr := parseClientPeer(spec)
		if parseErr != nil {
			return nil, parseErr
		}
		cfg.clients = append(cfg.clients, cp)
	}
	if len(cfg.clients) == 0 {
		return nil, errors.New("no client peers configured (--clients / AEGIS_CLIENTS, format: pubkey@10.8.0.2/32,...)")
	}

	if cfg.subnetA, err = netip.ParsePrefix(*subnetAStr); err != nil {
		return nil, fmt.Errorf("parse --subnet-a: %w", err)
	}
	if cfg.subnetB, err = netip.ParsePrefix(*subnetBStr); err != nil {
		return nil, fmt.Errorf("parse --subnet-b: %w", err)
	}
	if cfg.entryAddr, err = netip.ParseAddr(*entryAStr); err != nil {
		return nil, fmt.Errorf("parse --entry-addr: %w", err)
	}
	if cfg.innerAddr, err = netip.ParseAddr(*innerBStr); err != nil {
		return nil, fmt.Errorf("parse --inner-addr: %w", err)
	}
	return &cfg, nil
}

func intEnvOr(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func keyFromFlag(s, name string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("--%s must be base64 of a 32-byte WireGuard key", name)
	}
	return raw, nil
}

func parseClientPeer(spec string) (clientPeer, error) {
	pubStr, ipStr, ok := strings.Cut(spec, "@")
	if !ok {
		return clientPeer{}, fmt.Errorf("client peer %q must be pubkey@ip/32", spec)
	}
	pub, err := base64.StdEncoding.DecodeString(pubStr)
	if err != nil || len(pub) != 32 {
		return clientPeer{}, fmt.Errorf("client peer %q: pubkey must be base64 of 32 bytes", spec)
	}
	prefix, err := netip.ParsePrefix(ipStr)
	if err != nil {
		return clientPeer{}, fmt.Errorf("client peer %q: %w", spec, err)
	}
	if prefix.Bits() != prefix.Addr().BitLen() {
		return clientPeer{}, fmt.Errorf("client peer %q: allowed IP must be a host address (/32 or /128)", spec)
	}
	return clientPeer{pubkey: pub, ip: prefix.Addr()}, nil
}

func genPrivateKey() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		log.Fatalf("relay: entropy failure: %v", err)
	}
	// Clamp exactly like `wg genkey` (RFC 7748 + WireGuard convention).
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	return k
}

func pubB64(priv []byte) string {
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "<invalid>"
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func run(cfg *config) error {
	logLevel := device.LogLevelError
	if cfg.verbose {
		logLevel = device.LogLevelVerbose
	}

	// Two userspace tun devices bridged by the router (see ustun.go/router.go).
	tunA := newUSTun("aegis0", cfg.mtu)
	tunB := newUSTun("aegis1", cfg.mtu)

	rt := newRouter(cfg.entryAddr, cfg.innerAddr)
	rt.addRoute(cfg.subnetA, tunA) // clients live on subnet A
	rt.addRoute(cfg.subnetB, tunB) // inner WG link to the exit node
	rt.addRoute(netip.MustParsePrefix("0.0.0.0/0"), tunB)
	if cfg.enableV6 {
		rt.addRoute(netip.MustParsePrefix("::/0"), tunB)
	}

	// ---- device A: client-facing entry --------------------------------
	devA := device.NewDevice(tunA, conn.NewDefaultBind(), device.NewLogger(logLevel, "relay/entry "))
	defer devA.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", hex.EncodeToString(cfg.entryPrivateKey))
	fmt.Fprintf(&b, "listen_port=%d\n", cfg.entryListenPort)
	b.WriteString("replace_peers=true\n")
	for _, cp := range cfg.clients {
		fmt.Fprintf(&b, "public_key=%s\n", hex.EncodeToString(cp.pubkey))
		fmt.Fprintf(&b, "allowed_ip=%s/%d\n", cp.ip, cp.ip.BitLen())
	}
	if err := devA.IpcSet(b.String()); err != nil {
		return fmt.Errorf("configure entry device: %w", err)
	}

	// ---- device B: exit-side link -------------------------------------
	devB := device.NewDevice(tunB, conn.NewDefaultBind(), device.NewLogger(logLevel, "relay/exit "))
	defer devB.Close()

	b.Reset()
	fmt.Fprintf(&b, "private_key=%s\n", hex.EncodeToString(cfg.exitPrivateKey))
	b.WriteString("listen_port=0\n") // ephemeral source port; exit learns the endpoint from the handshake
	b.WriteString("replace_peers=true\n")
	fmt.Fprintf(&b, "public_key=%s\n", hex.EncodeToString(cfg.exitPubkey))
	fmt.Fprintf(&b, "endpoint=%s\n", cfg.exitEndpoint)
	b.WriteString("persistent_keepalive_interval=25\n")
	b.WriteString("replace_allowed_ips=true\n")
	b.WriteString("allowed_ip=0.0.0.0/0\n")
	if cfg.enableV6 {
		b.WriteString("allowed_ip=::/0\n")
	}
	if err := devB.IpcSet(b.String()); err != nil {
		return fmt.Errorf("configure exit device: %w", err)
	}

	if err := devA.Up(); err != nil {
		return fmt.Errorf("bring up entry device: %w", err)
	}
	if err := devB.Up(); err != nil {
		return fmt.Errorf("bring up exit device: %w", err)
	}

	// ---- userspace forwarding -----------------------------------------
	stop := make(chan struct{})
	selfAddrs := []netip.Addr{cfg.entryAddr, cfg.innerAddr}
	go rt.forward(tunA, "entry", selfAddrs, stop) // client -> exit
	go rt.forward(tunB, "exit", selfAddrs, stop)  // exit -> client

	// ---- status endpoint ----------------------------------------------
	if cfg.statusListen != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"service":"aegis-relay"}` + "\n"))
		})
		mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
			s := &rt.stats
			fmt.Fprintf(w, `{"entryRxPackets":%d,"entryTxPackets":%d,"exitRxPackets":%d,"exitTxPackets":%d,`+
				`"echoReplies":%d,"ttlExpired":%d,"noRouteDrops":%d,"queueDrops":%d}`+"\n",
				s.entryRxPackets.Load(), s.entryTxPackets.Load(), s.exitRxPackets.Load(), s.exitTxPackets.Load(),
				s.echoReplies.Load(), s.ttlExpired.Load(), s.noRouteDrops.Load(), s.queueDrops.Load())
		})
		srv := &http.Server{Addr: cfg.statusListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			log.Printf("relay: status endpoint on %s (/healthz, /stats)", cfg.statusListen)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("relay: status endpoint: %v", err)
			}
		}()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
		}()
	}

	log.Printf("relay: Double VPN relay running")
	log.Printf("relay:  entry WG listening on :%d (relay entry pubkey %s)", cfg.entryListenPort, pubB64(cfg.entryPrivateKey))
	log.Printf("relay:  inner WG link to %s (relay inner pubkey %s)", cfg.exitEndpoint, pubB64(cfg.exitPrivateKey))
	log.Printf("relay:  subnets: A=%s (entry %s), B=%s (inner %s)", cfg.subnetA, cfg.entryAddr, cfg.subnetB, cfg.innerAddr)
	for _, cp := range cfg.clients {
		log.Printf("relay:  client peer %s at %s", pubB64(cp.pubkey), cp.ip)
	}

	// ---- shutdown ------------------------------------------------------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("relay: received %s, shutting down", sig)
	close(stop)
	devB.Close()
	devA.Close()
	select {
	case <-devA.Wait():
	case <-time.After(3 * time.Second):
		log.Printf("relay: forced shutdown (entry device did not drain in time)")
	}
	return nil
}
