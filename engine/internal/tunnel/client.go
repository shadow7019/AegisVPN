// Package aegis is the AegisVPN engine core: a library that establishes real
// WireGuard tunnels using golang.zx2c4.com/wireguard (device/conn/tun), takes
// over host networking through internal/router, runs the Threat Protection
// DNS forwarder and orchestrates Double VPN chains.
//
// The API is the contract of docs/PROTOCOL.md section 1; it is re-exported at
// the module root (engine/aegis.go) and consumed by the desktop GUIs, the
// daemon CLI and the gomobile Android binding.
package aegis

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/shadow7019/AegisVPN/engine/internal/config"
	"github.com/shadow7019/AegisVPN/engine/internal/dns"
	"github.com/shadow7019/AegisVPN/engine/internal/features"
	"github.com/shadow7019/AegisVPN/engine/internal/obfs"
	"github.com/shadow7019/AegisVPN/engine/internal/registry"
	"github.com/shadow7019/AegisVPN/engine/internal/router"
)

// Version is the engine version reported to the control API and CLI.
const Version = "1.0.0"

const (
	// fwmark is the SO_MARK set on WireGuard's own UDP sockets so Linux
	// policy routing can keep the encrypted control traffic out of the
	// tunnel (wg-quick's approach).
	fwmark = 51820
	// handshakeTimeout is the maximum wait for a first handshake while
	// connecting before the attempt is declared failed.
	handshakeTimeout = 90 * time.Second
	// statsInterval is how often device UAPI stats are polled.
	statsInterval = 2 * time.Second
	// defaultMTU is the wg-quick default for a single hop.
	defaultMTU = 1420
	// doubleHopOverhead accounts for one extra WireGuard encapsulation
	// (outer IP+UDP+WG headers ≈ 80 bytes) in a Double VPN chain.
	doubleHopOverhead = 80
)

// Tunnel states (Status.State).
const (
	StateDisconnected  = "disconnected"
	StateConnecting    = "connecting"
	StateConnected     = "connected"
	StateDisconnecting = "disconnecting"
)

// Status is the public tunnel status (PROTOCOL.md section 1).
type Status struct {
	State            string `json:"state"`
	Profile          string `json:"profile,omitempty"`
	Node             string `json:"node,omitempty"`
	LocalIP          string `json:"localIp,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
	Hops             int    `json:"hops"`
	KillSwitch       bool   `json:"killSwitch"`
	ThreatProtection bool   `json:"threatProtection"`
	RxBytes          uint64 `json:"rxBytes"`
	TxBytes          uint64 `json:"txBytes"`
	HandshakeAt      int64  `json:"handshakeAt"`
	LatencyMs        int    `json:"latencyMs,omitempty"`
	Error            string `json:"error,omitempty"`
}

// ConnectOpts configures Connect (PROTOCOL.md section 1).
type ConnectOpts struct {
	Profile          string   `json:"profile"`
	Node             string   `json:"node,omitempty"`
	Relay            string   `json:"relay,omitempty"`
	KillSwitch       bool     `json:"killSwitch"`
	ThreatProtection bool     `json:"threatProtection"`
	SplitExclude     []string `json:"splitExclude,omitempty"`
	SplitInclude     []string `json:"splitInclude,omitempty"`
	Obfuscate        bool     `json:"obfuscate"`
	ObfsKey          string   `json:"obfsKey,omitempty"`
	AutoConnect      bool     `json:"autoConnect"`
}

// Client is the engine library handle.
type Client struct {
	root    string
	reg     *registry.Client
	opMu    sync.Mutex // serialises Connect/Disconnect/Close
	mu      sync.Mutex // guards st
	st      Status     // current status snapshot
	dev     *device.Device
	tunDev  tun.Device
	rtr     router.Manager // tunnel lifecycle (routes, DNS, split)
	ksRtr   router.Manager // kill switch (persistent across tunnels)
	dnsSrv  *dns.Server
	stopCh  chan struct{} // closed to stop the stats goroutine
	dnsPort int
}

// New creates a Client rooted at appRoot. Profiles, the keychain and DNS
// blocklists live in subdirectories of appRoot.
func New(appRoot string) (*Client, error) {
	if appRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		appRoot = filepath.Join(home, ".aegisvpn")
	}
	for _, d := range []string{"", "profiles", "keychain", "blocklists"} {
		if err := os.MkdirAll(filepath.Join(appRoot, d), 0o700); err != nil {
			return nil, fmt.Errorf("aegis: create %s: %w", appRoot, err)
		}
	}
	return &Client{
		root:    appRoot,
		reg:     registry.New(appRoot),
		ksRtr:   router.New(),
		dnsPort: dnsPortFromEnv(),
		st:      Status{State: StateDisconnected},
	}, nil
}

// Root returns the application data root.
func (c *Client) Root() string { return c.root }

// Registry exposes the node registry client (used by the control API).
func (c *Client) Registry() *registry.Client { return c.reg }

// Status returns a snapshot of the current tunnel status.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

func (c *Client) setStatus(mut func(*Status)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mut(&c.st)
}

// Connect establishes the tunnel described by opts.
func (c *Client) Connect(opts ConnectOpts) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if s := c.Status().State; s != StateDisconnected {
		return fmt.Errorf("aegis: cannot connect from state %q", s)
	}
	c.setStatus(func(s *Status) {
		s.State = StateConnecting
		s.Error = ""
		s.RxBytes, s.TxBytes, s.HandshakeAt = 0, 0, 0
		s.Node = opts.Node
		s.Hops = 1
		s.Profile = profileLabel(opts)
		// A kill switch already armed (or newly requested) stays armed.
		s.KillSwitch = s.KillSwitch || opts.KillSwitch
	})

	// Always tear down on failure so the host is left in a clean state.
	if err := c.connectInner(opts); err != nil {
		c.teardown()
		c.setStatus(func(s *Status) {
			s.State = StateDisconnected
			s.Error = err.Error()
		})
		return err
	}
	return nil
}

// profileLabel derives the Status.Profile label from the opts.
func profileLabel(opts ConnectOpts) string {
	if opts.Profile != "" {
		if strings.Contains(opts.Profile, "\n") || strings.Contains(opts.Profile, "[Interface]") {
			return "inline"
		}
		return filepath.Base(opts.Profile)
	}
	if opts.Node != "" {
		return opts.Node
	}
	return ""
}

func (c *Client) connectInner(opts ConnectOpts) error {
	// 1. Resolve the profile (file path, inline wg-quick text, or node ID).
	profile, err := c.resolveProfile(opts)
	if err != nil {
		return err
	}

	// 2. Double VPN: rewrite into a 2-peer chain when a relay is set.
	hops := 1
	if opts.Relay != "" {
		profile, err = c.buildDoubleHop(profile, opts)
		if err != nil {
			return err
		}
		hops = 2
	}

	// 3. Effective MTU (obfuscation options are applied in connectInnerTUN).
	mtu := profile.MTU
	if mtu <= 0 {
		mtu = defaultMTU
	}
	if hops == 2 && mtu > defaultMTU-doubleHopOverhead {
		mtu = defaultMTU - doubleHopOverhead
	}

	return c.connectInnerTUN(opts, profile, hops, mtu, -1)
}

// connectInnerTUN performs the device setup for a resolved profile. When
// tunFD >= 0 (Android VpnService) the platform owns routes/DNS/kill switch
// and only the WireGuard device is managed here.
func (c *Client) connectInnerTUN(opts ConnectOpts, profile *config.Profile, hops, mtu, tunFD int) error {
	// 4. Create the real TUN device. golang.zx2c4.com/wireguard/tun handles
	//    per-OS backends: /dev/net/tun (linux), WireGuardNT/WinTun
	//    (windows), utun (darwin), or an externally provided fd (Android).
	useObfs := opts.Obfuscate || profile.Obfs
	obfsKey := opts.ObfsKey
	if obfsKey == "" {
		obfsKey = profile.ObfsKey
	}
	var tunDev tun.Device
	var err error
	if tunFD >= 0 {
		tunDev, err = createTUNFromFile(tunFD, mtu)
	} else {
		tunDev, err = tun.CreateTUN(tunDeviceName(), mtu)
	}
	if err != nil {
		return fmt.Errorf("aegis: create tun: %w", err)
	}
	c.tunDev = tunDev
	tunName, err := tunDev.Name()
	if err != nil {
		return fmt.Errorf("aegis: tun name: %w", err)
	}

	// 5. Create the UDP bind (optionally wrapped in the XOR obfuscator).
	var bind conn.Bind = conn.NewDefaultBind()
	if useObfs {
		if obfsKey == "" {
			return fmt.Errorf("aegis: obfuscation requested but no key provided")
		}
		bind = obfs.NewBind(obfsKey, bind)
	}

	// 6. Create the WireGuard device.
	dev := device.NewDevice(tunDev, bind, device.NewLogger(device.LogLevelError, "aegisvpn: "))
	c.dev = dev

	// 7. Apply the UAPI config. On Linux we attempt fwmark policy routing;
	//    if SO_MARK is unavailable we retry without it and let the router
	//    fall back to endpoint host routes + plain default routes.
	useFwmark := runtime.GOOS == "linux" && tunFD < 0
	mark := uint32(0)
	if useFwmark {
		mark = fwmark
	}
	uapi, err := profile.IpcConfig(mark)
	if err != nil {
		return fmt.Errorf("aegis: build uapi config: %w", err)
	}
	if err := dev.IpcSet(uapi); err != nil {
		if !useFwmark {
			return fmt.Errorf("aegis: apply config: %w", err)
		}
		useFwmark = false
		uapi, err = profile.IpcConfig(0)
		if err != nil {
			return fmt.Errorf("aegis: build uapi config: %w", err)
		}
		if err := dev.IpcSet(uapi); err != nil {
			return fmt.Errorf("aegis: apply config: %w", err)
		}
	}

	// 8. Take over host networking (skipped on Android: VpnService.Builder
	//    owns routes, DNS and per-app split tunneling there).
	endpoints := resolvedEndpoints(profile)
	var rtr router.Manager
	if tunFD >= 0 {
		c.finishConnect(profile, opts, hops, dev, tunName, endpoints)
		return nil
	}
	rtr = router.New()
	c.rtr = rtr
	if es, ok := rtr.(router.EndpointSetter); ok {
		es.SetEndpoints(endpoints)
	}
	if fs, ok := rtr.(router.FwmarkSetter); ok {
		fs.SetFwmark(fwmark, useFwmark)
	}
	// The kill switch manager also needs endpoint exceptions so a re-armed
	// lockdown keeps allowing the (encrypted) WireGuard control traffic.
	if es, ok := c.ksRtr.(router.EndpointSetter); ok {
		es.SetEndpoints(endpoints)
	}
	addrs, err := parseCIDRs(profile.Addresses)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("aegis: profile has no Address to assign")
	}
	if err := rtr.Up(tunName, addrs, parseCIDRsLoose(profile.AllowedIPs()), mtu); err != nil {
		return fmt.Errorf("aegis: router up: %w", err)
	}

	// 9. Kill switch: (re-)arm via the persistent kill-switch manager with
	//    the tunnel interface so traffic is allowed only through it.
	if c.Status().KillSwitch {
		if err := c.ksRtr.KillSwitch(true, tunName); err != nil {
			return fmt.Errorf("aegis: kill switch: %w", err)
		}
	}

	// 10. DNS: Threat Protection sinkhole or plain tunnel DNS.
	dnsServers := profile.DNS
	if opts.ThreatProtection {
		upstream := "10.8.0.1"
		if len(dnsServers) > 0 {
			upstream = dnsServers[0]
		}
		srv := dns.New(c.dnsPort, upstream)
		_ = srv.LoadBlocklists(filepath.Join(c.root, "blocklists"))
		srv.SetRules(features.SmartPlayRules())
		if err := srv.Enable(); err != nil {
			// Port busy or not privileged: degrade to plain tunnel DNS
			// rather than failing the whole connect.
			srv.Close()
		} else {
			c.dnsSrv = srv
			dnsServers = []string{"127.0.0.1"}
		}
	}
	c.setStatus(func(s *Status) { s.ThreatProtection = c.dnsSrv != nil })
	if len(dnsServers) > 0 {
		if err := rtr.SetDNS(dnsServers); err != nil {
			return fmt.Errorf("aegis: dns takeover: %w", err)
		}
	}

	// 11. Split tunneling.
	exclude := parseCIDRsLoose(opts.SplitExclude)
	include := parseCIDRsLoose(opts.SplitInclude)
	if len(exclude) > 0 || len(include) > 0 {
		if err := rtr.SplitTunnel(exclude, include); err != nil {
			return fmt.Errorf("aegis: split tunnel: %w", err)
		}
	}

	// 12. Static status fields + stats/watchdog goroutine.
	localIP := ""
	if len(profile.Addresses) > 0 {
		localIP = strings.SplitN(profile.Addresses[0], "/", 2)[0]
	}
	endpoint := ""
	if len(profile.Peers) > 0 {
		endpoint = profile.Peers[0].Endpoint
	}
	latency := 0
	if opts.Node != "" {
		if n, err := c.reg.Find(opts.Node); err == nil {
			latency = n.LatencyMs
		}
	}
	c.setStatus(func(s *Status) {
		s.Hops = hops
		s.LocalIP = localIP
		s.Endpoint = endpoint
		s.LatencyMs = latency
	})
	c.stopCh = make(chan struct{})
	go c.statsLoop(dev, time.Now())
	return nil
}

// tunDeviceName picks the tunnel interface name per OS. The wireguard tun
// package creates: "aegisvpn0" via /dev/net/tun (linux), the "AegisVPN"
// WireGuardNT/WinTun adapter (windows), "utun9" (darwin).
func tunDeviceName() string {
	switch runtime.GOOS {
	case "windows":
		return "AegisVPN"
	case "darwin":
		return "utun9"
	default:
		return "aegisvpn0"
	}
}

// resolveProfile turns opts into a parsed profile: inline wg-quick text,
// a .conf file path, or a registry node ID (client key material is then
// provisioned/stored under appRoot/keychain).
func (c *Client) resolveProfile(opts ConnectOpts) (*config.Profile, error) {
	if opts.Profile != "" {
		if strings.Contains(opts.Profile, "[Interface]") {
			return config.Parse(opts.Profile)
		}
		return config.LoadFile(opts.Profile)
	}
	if opts.Node == "" {
		return nil, fmt.Errorf("aegis: connect requires a profile or a node")
	}
	node, err := c.reg.Find(opts.Node)
	if err != nil {
		return nil, err
	}
	return c.nodeProfile(node)
}

// nodeProfile builds a profile for a registry node, persisting a freshly
// generated client keypair in the keychain directory (keys never leave the
// engine — PROTOCOL.md section 6.4).
func (c *Client) nodeProfile(node *registry.Node) (*config.Profile, error) {
	keyPath := filepath.Join(c.root, "keychain", "client.key")
	var priv string
	if data, err := os.ReadFile(keyPath); err == nil {
		if f := strings.Fields(string(data)); len(f) > 0 {
			priv = f[0]
		}
	}
	if priv == "" {
		var err error
		priv, _, err = config.GenerateKey()
		if err != nil {
			return nil, err
		}
		pub, err := derivePublic(priv)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, []byte(priv+"\n"+pub+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	return &config.Profile{
		PrivateKey: priv,
		Addresses:  []string{"10.8.0.2/32"},
		DNS:        []string{"10.8.0.1"},
		Peers: []*config.Peer{{
			PublicKey:  node.Pubkey,
			Endpoint:   fmt.Sprintf("%s:%d", node.Host, node.Port),
			AllowedIPs: []string{"0.0.0.0/0", "::/0"},
		}},
	}, nil
}

// buildDoubleHop rewrites the profile into a genuine two-peer WireGuard
// chain (Double VPN).
//
// How the chain works with a single client device and two peers:
//
//	entry peer (first hop):  Endpoint = entry node, AllowedIPs = exitIP/32
//	exit  peer (second hop): Endpoint = exit node,  AllowedIPs = 0.0.0.0/0,::/0
//
// Encrypted packets for the exit node are ordinary UDP datagrams addressed to
// the exit endpoint; because the OS routes that destination into the tunnel
// (default route via tun), those datagrams re-enter the TUN and the device
// cryptokey-routes them (dst = exitIP) to the entry peer — i.e. the WG-UDP
// packet to the exit is itself the payload encrypted to the entry peer. The
// relay (entry) server runs standard WireGuard with IP forwarding enabled and
// routes the client's tunnel subnet to the exit peer; the exit node sees the
// relay's address as its WireGuard peer and routes the client subnet back
// through the relay. Result: nested WireGuard encryption
// (payload → exit key → UDP-to-exit → entry key), with both hops' public
// keys required here.
//
// Entry peer selection: opts.Node (registry) if provided, otherwise the
// single peer already present in the profile. opts.Relay selects the exit
// ("second hop") node from the registry.
func (c *Client) buildDoubleHop(profile *config.Profile, opts ConnectOpts) (*config.Profile, error) {
	exit, err := c.reg.Find(opts.Relay)
	if err != nil {
		return nil, fmt.Errorf("aegis: relay node: %w", err)
	}
	exitIP, err := resolveHost(exit.Host)
	if err != nil {
		return nil, fmt.Errorf("aegis: relay endpoint: %w", err)
	}
	exitEP := fmt.Sprintf("%s:%d", exit.Host, exit.Port)

	var entry *config.Peer
	switch {
	case opts.Node != "":
		node, err := c.reg.Find(opts.Node)
		if err != nil {
			return nil, fmt.Errorf("aegis: entry node: %w", err)
		}
		entry = &config.Peer{
			PublicKey:  node.Pubkey,
			Endpoint:   fmt.Sprintf("%s:%d", node.Host, node.Port),
			AllowedIPs: []string{exitIP.String() + "/32"},
		}
	case len(profile.Peers) == 1:
		e := *profile.Peers[0]
		e.AllowedIPs = []string{exitIP.String() + "/32"}
		entry = &e
	default:
		return nil, fmt.Errorf("aegis: relay requires a single-peer profile or --node")
	}

	return &config.Profile{
		PrivateKey: profile.PrivateKey,
		Addresses:  profile.Addresses,
		DNS:        profile.DNS,
		MTU:        profile.MTU,
		Obfs:       profile.Obfs,
		ObfsKey:    profile.ObfsKey,
		Peers: []*config.Peer{
			entry,
			{
				PublicKey:           exit.Pubkey,
				Endpoint:            exitEP,
				AllowedIPs:          []string{"0.0.0.0/0", "::/0"},
				PersistentKeepalive: 25, // keep the relay-side mapping alive
			},
		},
	}, nil
}

// resolveHost resolves a host to a netip.Addr (for the /32 cryptokey route).
func resolveHost(host string) (netip.Addr, error) {
	if ip := net.ParseIP(host); ip != nil {
		a, ok := netip.AddrFromSlice(ip)
		if !ok {
			return netip.Addr{}, fmt.Errorf("bad IP %s", host)
		}
		return a.Unmap(), nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, a := range addrs {
		if v4 := a.To4(); v4 != nil {
			out, ok := netip.AddrFromSlice(v4)
			if ok {
				return out, nil
			}
		}
	}
	out, ok := netip.AddrFromSlice(addrs[0])
	if !ok {
		return netip.Addr{}, fmt.Errorf("no usable address for %s", host)
	}
	return out, nil
}

// resolvedEndpoints converts the profile peers' endpoints to AddrPorts.
func resolvedEndpoints(profile *config.Profile) []netip.AddrPort {
	var out []netip.AddrPort
	for _, ep := range profile.EndpointHosts() {
		for _, peer := range profile.Peers {
			_, portStr, err := net.SplitHostPort(peer.Endpoint)
			if err != nil {
				continue
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				continue
			}
			a, ok := netip.AddrFromSlice(ep)
			if !ok {
				continue
			}
			out = append(out, netip.AddrPortFrom(a.Unmap(), uint16(port)))
			break
		}
	}
	return out
}

func parseCIDRs(list []string) ([]net.IPNet, error) {
	var out []net.IPNet
	for _, s := range list {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("aegis: bad CIDR %q: %w", s, err)
		}
		out = append(out, *n)
	}
	return out, nil
}

func parseCIDRsLoose(list []string) []net.IPNet {
	var out []net.IPNet
	for _, s := range list {
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, *n)
		}
	}
	return out
}

// statsLoop polls device.IpcGet for rx/tx bytes and handshake times, drives
// the connecting→connected transition and enforces the 90s handshake timeout.
func (c *Client) statsLoop(dev *device.Device, started time.Time) {
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
		}
		uapi, err := dev.IpcGet()
		if err != nil {
			return // device closed
		}
		var rx, tx uint64
		var hsSec int64
		for _, line := range strings.Split(uapi, "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch k {
			case "rx_bytes":
				if n, err := strconv.ParseUint(v, 10, 64); err == nil {
					rx += n
				}
			case "tx_bytes":
				if n, err := strconv.ParseUint(v, 10, 64); err == nil {
					tx += n
				}
			case "last_handshake_time_sec":
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > hsSec {
					hsSec = n
				}
			}
		}
		state := c.Status().State
		if state != StateConnecting && state != StateConnected {
			return
		}
		if hsSec > 0 {
			c.setStatus(func(s *Status) {
				s.RxBytes, s.TxBytes, s.HandshakeAt = rx, tx, hsSec
				s.State = StateConnected
				s.Error = ""
			})
			continue
		}
		c.setStatus(func(s *Status) { s.RxBytes, s.TxBytes = rx, tx })
		if state == StateConnecting && time.Since(started) > handshakeTimeout {
			_ = c.Disconnect()
			c.setStatus(func(s *Status) {
				s.State = StateDisconnected
				s.Error = fmt.Sprintf("no WireGuard handshake within %s (wrong key, blocked UDP, or dead endpoint)", handshakeTimeout)
			})
			return
		}
	}
}

// teardown stops the stats loop, the DNS forwarder, the tunnel router and the
// WireGuard device. A kill switch that should stay armed is re-armed in
// offline mode (block-all without tunnel exception).
func (c *Client) teardown() {
	if c.stopCh != nil {
		select {
		case <-c.stopCh:
		default:
			close(c.stopCh)
		}
		c.stopCh = nil
	}
	if c.dnsSrv != nil {
		c.dnsSrv.Close()
		c.dnsSrv = nil
	}
	keepKS := c.Status().KillSwitch
	if c.rtr != nil {
		c.rtr.ResetDNS()
		c.rtr.Down()
		c.rtr = nil
	}
	// Re-arm the kill switch without the (now gone) tunnel interface.
	if keepKS {
		_ = c.ksRtr.KillSwitch(false, "")
		_ = c.ksRtr.KillSwitch(true, "")
	}
	if c.dev != nil {
		c.dev.Close()
		c.dev = nil
		c.tunDev = nil
	}
}

// Disconnect tears the tunnel down and returns to the disconnected state.
func (c *Client) Disconnect() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if c.Status().State == StateDisconnected {
		return nil
	}
	c.setStatus(func(s *Status) { s.State = StateDisconnecting })
	c.teardown()
	c.setStatus(func(s *Status) {
		s.State = StateDisconnected
		s.LocalIP = ""
		s.Endpoint = ""
		s.Hops = 0
		s.RxBytes, s.TxBytes, s.HandshakeAt = 0, 0, 0
	})
	return nil
}

// SetKillSwitch enables/disables the firewall lockdown. The kill switch may
// stay armed even while disconnected (block-all with no tunnel).
func (c *Client) SetKillSwitch(enabled bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	tunName := ""
	if c.tunDev != nil {
		if n, err := c.tunDev.Name(); err == nil {
			tunName = n
		}
	}
	if err := c.ksRtr.KillSwitch(enabled, tunName); err != nil {
		return err
	}
	c.setStatus(func(s *Status) { s.KillSwitch = enabled })
	return nil
}

// SetThreatProtection toggles the sinkhole DNS forwarder; enabling it also
// repoints system DNS at 127.0.0.1.
func (c *Client) SetThreatProtection(enabled bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if enabled {
		if c.dnsSrv != nil {
			return nil
		}
		upstream := "10.8.0.1"
		if servers := c.currentProfileDNS(); len(servers) > 0 && isIP(servers[0]) {
			upstream = servers[0]
		}
		srv := dns.New(c.dnsPort, upstream)
		_ = srv.LoadBlocklists(filepath.Join(c.root, "blocklists"))
		srv.SetRules(features.SmartPlayRules())
		if err := srv.Enable(); err != nil {
			srv.Close()
			return fmt.Errorf("aegis: threat protection DNS: %w", err)
		}
		c.dnsSrv = srv
		c.setStatus(func(s *Status) { s.ThreatProtection = true })
		if c.rtr != nil {
			return c.rtr.SetDNS([]string{"127.0.0.1"})
		}
		return nil
	}
	if c.dnsSrv == nil {
		c.setStatus(func(s *Status) { s.ThreatProtection = false })
		return nil
	}
	_ = c.dnsSrv.Close()
	c.dnsSrv = nil
	c.setStatus(func(s *Status) { s.ThreatProtection = false })
	if c.rtr != nil {
		if servers := c.currentProfileDNS(); len(servers) > 0 {
			return c.rtr.SetDNS(servers)
		}
		return c.rtr.ResetDNS()
	}
	return nil
}

// currentProfileDNS returns the DNS servers of the last-used profile.
func (c *Client) currentProfileDNS() []string {
	label := c.Status().Profile
	if label == "" || label == "inline" {
		return nil
	}
	path := filepath.Join(c.root, "profiles", label)
	if _, err := os.Stat(path); err != nil {
		path = label
	}
	profile, err := config.LoadFile(path)
	if err != nil {
		return nil
	}
	return profile.DNS
}

// SetSplitTunnel applies exclude (bypass tunnel) / include (force through
// tunnel) routes while connected.
func (c *Client) SetSplitTunnel(excludeCIDRs, includeCIDRs []string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if c.rtr == nil {
		return fmt.Errorf("aegis: split tunnel requires an active tunnel")
	}
	return c.rtr.SplitTunnel(parseCIDRsLoose(excludeCIDRs), parseCIDRsLoose(includeCIDRs))
}

// ApplyDNS performs a system DNS takeover with the given servers.
func (c *Client) ApplyDNS(servers []string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if c.rtr == nil {
		return fmt.Errorf("aegis: DNS takeover requires an active tunnel")
	}
	return c.rtr.SetDNS(servers)
}

// Close disconnects and releases everything.
func (c *Client) Close() error {
	_ = c.Disconnect()
	if c.dnsSrv != nil {
		c.dnsSrv.Close()
		c.dnsSrv = nil
	}
	return nil
}

// dnsPortFromEnv reads AEGIS_DNS_PORT (default 53).
func dnsPortFromEnv() int {
	if v := os.Getenv("AEGIS_DNS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 65536 {
			return n
		}
	}
	return 53
}

func isIP(s string) bool { return net.ParseIP(s) != nil }

// derivePublic returns the base64 public key for a base64 private key.
func derivePublic(privB64 string) (string, error) {
	return config.PublicFromPrivate(privB64)
}

// finishConnect finalises status and starts the stats watchdog for the
// platform-managed TUN path (Android VpnService), where routes, DNS and
// per-app split tunneling are owned by the OS rather than the router.
func (c *Client) finishConnect(profile *config.Profile, opts ConnectOpts, hops int, dev *device.Device, tunName string, endpoints []netip.AddrPort) {
	_ = tunName
	_ = endpoints
	localIP := ""
	if len(profile.Addresses) > 0 {
		localIP = strings.SplitN(profile.Addresses[0], "/", 2)[0]
	}
	endpoint := ""
	if len(profile.Peers) > 0 {
		endpoint = profile.Peers[0].Endpoint
	}
	latency := 0
	if opts.Node != "" {
		if n, err := c.reg.Find(opts.Node); err == nil {
			latency = n.LatencyMs
		}
	}
	c.setStatus(func(s *Status) {
		s.Hops = hops
		s.LocalIP = localIP
		s.Endpoint = endpoint
		s.LatencyMs = latency
	})
	c.stopCh = make(chan struct{})
	go c.statsLoop(dev, time.Now())
}

// ParseProfile parses inline wg-quick profile text (gomobile-friendly).
func ParseProfile(wgQuickText string) (*config.Profile, error) {
	return config.Parse(wgQuickText)
}

// StartWithFD connects the WireGuard device over an externally provided TUN
// file descriptor (Android VpnService.Builder.establish()). The platform
// owns routes, DNS and per-app split tunneling; Threat Protection is served
// by the tunnel-side threatdns node (10.8.0.1). Android must call
// VpnService.protect() semantics via setUnderlyingNetworks(null) and keep
// the service alive for the tunnel duration.
func StartWithFD(c *Client, fd int, prof *config.Profile, obfsEnabled bool, obfsKey string) error {
	opts := ConnectOpts{Obfuscate: obfsEnabled, ObfsKey: obfsKey}
	mtu := prof.MTU
	if mtu <= 0 {
		mtu = defaultMTU
	}
	return c.connectInnerTUN(opts, prof, 1, mtu, fd)
}
