// Command aegisvpn-server hosts the AegisVPN browser dashboard together with
// the real WireGuard engine. It is the main executable of the macOS bundle:
//
//      double-click AegisVPN.app
//        └─ aegisvpn-server  (this binary)
//             ├─ already running?  → focus the dashboard tab
//             ├─ ask for administrator rights once (utun needs root)
//             │    └─ re-exec self as root:  --child --root /var/root/.aegisvpn
//             ├─ wait for the local HTTP listener
//             └─ open http://127.0.0.1:8080 in the default browser
//
// The elevated child owns the engine client (aegis package) exactly like the
// Wails desktop shell does on Windows; the HTTP surface only ever binds to
// 127.0.0.1 so nothing leaves the machine.
package main

import (
        "flag"
        "fmt"
        "net"
        "net/http"
        "os"
        "os/exec"
        "os/signal"
        "path/filepath"
        "runtime"
        "strings"
        "syscall"
        "time"

        "github.com/shadow7019/AegisVPN/gui/desktop/webapp"
)

const (
        darwinRoot    = "/var/root/.aegisvpn"
        darwinLogPath = "/var/root/Library/Logs/AegisVPN-server.log"
)

func defaultRoot() string {
        switch runtime.GOOS {
        case "windows":
                local := os.Getenv("LOCALAPPDATA")
                if local == "" {
                        home, err := os.UserHomeDir()
                        if err != nil {
                                return "."
                        }
                        local = filepath.Join(home, "AppData", "Local")
                }
                return filepath.Join(local, "AegisVPN")
        case "darwin":
                home, err := os.UserHomeDir()
                if err != nil {
                        return darwinRoot
                }
                return filepath.Join(home, "Library", "Application Support", "AegisVPN")
        default:
                if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
                        return filepath.Join(dir, "aegisvpn")
                }
                home, err := os.UserHomeDir()
                if err != nil {
                        return "."
                }
                return filepath.Join(home, ".local", "share", "aegisvpn")
        }
}

// healthy reports whether an aegisvpn-server is already listening on addr.
func healthy(addr string) bool {
        base := "http://" + addr
        resp, err := http.Get(base + "/api/health")
        if err != nil {
                return false
        }
        defer resp.Body.Close()
        return resp.StatusCode == http.StatusOK
}

// openBrowser opens url with the platform default browser.
func openBrowser(url string) {
        switch runtime.GOOS {
        case "darwin":
                _ = exec.Command("open", url).Start()
        case "windows":
                _ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
        default:
                _ = exec.Command("xdg-open", url).Start()
        }
}

// macAlert shows a visible GUI dialog (via osascript) — the app has no
// window, so without this a failed engine start looks like "nothing
// happens" when launched from Finder.
func macAlert(msg string) {
        script := fmt.Sprintf(
                `display dialog %q with title "AegisVPN" buttons {"OK"} default button 1 with icon caution`, msg)
        _ = exec.Command("osascript", "-e", script).Run()
}

// runDesktop is the double-click flow on darwin.
func runDesktop(addr string) int {
        if healthy(addr) {
                openBrowser("http://" + addr)
                return 0
        }

        self, err := os.Executable()
        if err != nil {
                self = os.Args[0]
        }
        self, _ = filepath.Abs(self)

        root := darwinRoot
        logPath := darwinLogPath
        childArgs := []string{"--child", "--root", root, "--http", addr}

        // mkdir, log prep and launch happen in one root shell; the server is
        // detached with nohup so the AppleScript returns immediately.
        inner := fmt.Sprintf(
                "mkdir -p %q $(dirname %q); nohup %q %s >> %q 2>&1 &",
                root, logPath, self, strings.Join(childArgs, " "), logPath,
        )
        script := fmt.Sprintf("do shell script %q with administrator privileges", inner)
        elevateErr := exec.Command("osascript", "-e", script).Run()

        if elevateErr != nil {
                // User cancelled (or osascript unavailable): run unprivileged so the
                // dashboard still works — connecting will surface the OS error.
                root = defaultRoot()
                cmd := exec.Command(self, "--child", "--root", root, "--http", addr)
                detach(cmd)
                if err := cmd.Start(); err != nil {
                        fmt.Fprintf(os.Stderr, "aegisvpn-server: cannot start engine: %v\n", err)
                        return 1
                }
                _ = cmd.Process.Release()
        }

        // Wait for the listener before opening a dead tab.
        deadline := time.Now().Add(20 * time.Second)
        for time.Now().Before(deadline) {
                if healthy(addr) {
                        openBrowser("http://" + addr)
                        return 0
                }
                time.Sleep(250 * time.Millisecond)
        }

        fmt.Fprintf(os.Stderr, "aegisvpn-server: engine did not become ready on %s\n", addr)
        // Best effort: show the log location so failures are diagnosable.
        if elevateErr == nil {
                fmt.Fprintf(os.Stderr, "aegisvpn-server: see %s for details\n", logPath)
        }
        macAlert("AegisVPN could not start its dashboard engine.\n\n" +
                "Try launching again. If it keeps failing, reinstall with the\n" +
                "one-line installer from the download page, or run in Terminal:\n" +
                "  /Applications/AegisVPN.app/Contents/MacOS/AegisVPN\n\n" +
                fmt.Sprintf("Logs: %s", logPath))
        return 1
}

func main() {
        var (
                root      = flag.String("root", "", "application data root (default per-OS)")
                addr      = flag.String("http", "127.0.0.1:8080", "listen address for the dashboard")
                child     = flag.Bool("child", false, "internal: already elevated, serve and block")
                focus     = flag.Bool("focus", false, "only open the dashboard URL")
                childRoot string
        )
        flag.StringVar(&childRoot, "child-root", "", "deprecated alias of --root")
        flag.Parse()

        if *root == "" {
                *root = childRoot
        }
        if *root == "" {
                *root = defaultRoot()
        }

        if *focus {
                openBrowser("http://" + *addr)
                return
        }

        // macOS double-click flow: elevate once, then serve from the root child.
        if runtime.GOOS == "darwin" && !*child {
                os.Exit(runDesktop(*addr))
        }

        // Refuse to listen on a non-loopback address — the control plane never
        // leaves the machine.
        host, port, err := net.SplitHostPort(*addr)
        if err != nil {
                host, port = "127.0.0.1", *addr
        }
        if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "" {
                fmt.Fprintf(os.Stderr, "aegisvpn-server: refusing to bind %q — dashboard is local-only\n", host)
                os.Exit(1)
        }

        app, err := webapp.New(*root)
        if err != nil {
                fmt.Fprintf(os.Stderr, "aegisvpn-server: %v\n", err)
                os.Exit(1)
        }

        srv := &http.Server{
                Addr:              fmt.Sprintf("%s:%s", host, port),
                Handler:           app.Handler(),
                ReadHeaderTimeout: 10 * time.Second,
        }

        ln, err := net.Listen("tcp", srv.Addr)
        if err != nil {
                // Port already in use by a healthy instance? Just focus the UI.
                if healthy(fmt.Sprintf("%s:%s", host, port)) {
                        openBrowser(fmt.Sprintf("http://%s:%s", host, port))
                        return
                }
                fmt.Fprintf(os.Stderr, "aegisvpn-server: listen %s: %v\n", srv.Addr, err)
                os.Exit(1)
        }

        fmt.Printf("aegisvpn dashboard on http://%s:%s (root %s, uid %d)\n",
                host, port, *root, os.Getuid())

        sig := make(chan os.Signal, 2)
        signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
        go func() {
                <-sig
                fmt.Println("shutting down")
                _ = ln.Close()
        }()

        if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
                fmt.Fprintf(os.Stderr, "aegisvpn-server: %v\n", err)
                app.Close()
                os.Exit(1)
        }
        app.Close()
}
