// Command aegisvpn is the AegisVPN engine CLI and daemon.
//
//	aegisvpn daemon   [--root DIR]             start the local control API
//	aegisvpn connect  [profile.conf] [flags]   establish a tunnel
//	aegisvpn disconnect                        tear the tunnel down
//	aegisvpn status                            print status JSON
//	aegisvpn nodes                             list registry nodes
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/shadow7019/AegisVPN/engine/internal/api"
	tunnel "github.com/shadow7019/AegisVPN/engine/internal/tunnel"
)

func defaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/var/lib/aegisvpn"
	}
	return filepath.Join(home, ".aegisvpn")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aegisvpn: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var root string
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.StringVar(&root, "root", defaultRoot(), "application data root")
	// connect flags
	var node, relay, obfsKey string
	var ks, threat, obfs bool
	if cmd == "connect" {
		fs.StringVar(&node, "node", "", "registry node id")
		fs.StringVar(&relay, "relay", "", "second-hop node id (Double VPN)")
		fs.BoolVar(&ks, "killswitch", false, "enable kill switch")
		fs.BoolVar(&threat, "threat", false, "enable Threat Protection DNS")
		fs.BoolVar(&obfs, "obfs", false, "obfuscate WireGuard UDP")
		fs.StringVar(&obfsKey, "obfs-key", "", "obfuscation passphrase")
	}
	_ = fs.Parse(os.Args[2:])

	client, err := tunnel.New(root)
	if err != nil {
		fatal("%v", err)
	}
	defer client.Close()

	switch cmd {
	case "daemon":
		srv, err := api.Listen(client)
		if err != nil {
			fatal("control api: %v", err)
		}
		if runtime.GOOS == "windows" {
			fmt.Printf("aegisvpn daemon listening on 127.0.0.1:%d (root %s)\n", api.ControlPort, root)
		} else {
			fmt.Printf("aegisvpn daemon listening on %s (root %s)\n", srv.SocketPath(), root)
		}
		sig := make(chan os.Signal, 2)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Println("shutting down")
		_ = client.Disconnect()
		_ = srv.Close()

	case "connect":
		opts := tunnel.ConnectOpts{
			Node:             node,
			Relay:            relay,
			KillSwitch:       ks,
			ThreatProtection: threat,
			Obfuscate:        obfs,
			ObfsKey:          obfsKey,
		}
		if fs.NArg() > 0 {
			p, absErr := filepath.Abs(fs.Arg(0))
			if absErr != nil {
				fatal("%v", absErr)
			}
			opts.Profile = p
		}
		if err := client.Connect(opts); err != nil {
			fatal("connect: %v", err)
		}
		printStatus(client)

	case "disconnect":
		if err := client.Disconnect(); err != nil {
			fatal("disconnect: %v", err)
		}
		printStatus(client)

	case "status":
		printStatus(client)

	case "nodes":
		nodes, err := client.Registry().List(false)
		if err != nil {
			fatal("nodes: %v", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(nodes); err != nil {
			fatal("%v", err)
		}

	case "version":
		fmt.Println(tunnel.Version)

	default:
		usage()
	}
}

func printStatus(client *tunnel.Client) {
	b, err := json.MarshalIndent(client.Status(), "", "  ")
	if err != nil {
		fatal("%v", err)
	}
	fmt.Println(string(b))
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: aegisvpn <command> [flags]

commands:
  daemon      start the local control API daemon
  connect     connect to a node or profile
  disconnect  disconnect the active tunnel
  status      print status JSON
  nodes       list registry nodes
  version     print engine version
`)
	os.Exit(2)
}
