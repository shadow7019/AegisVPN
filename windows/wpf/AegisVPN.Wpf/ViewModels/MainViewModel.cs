using System.IO;
using System.Windows;
using System.Windows.Threading;
using AegisVPN.Wpf.Models;
using AegisVPN.Wpf.Services;

namespace AegisVPN.Wpf.ViewModels;

/// <summary>Root VM: owns the engine client, the status poll loop, navigation
/// and the connect/disconnect flow (Quick connect, Double VPN, split tunnel).</summary>
public sealed class MainViewModel : ViewModelBase, IDisposable
{
    private readonly EngineClient _engine = new();
    private readonly DispatcherTimer _pollTimer;
    private readonly AppSettings _startupSettings;

    private VpnStatus _status = new();
    private string _currentNav = "dashboard";
    private string? _daemonVersion;
    private bool _transportAlive;

    // rate computation
    private ulong _lastRx;
    private ulong _lastTx;
    private long _lastTick;
    private string _downRate = "0 B/s";
    private string _upRate = "0 B/s";

    // session
    private DateTime? _connectedSince;

    public ServersViewModel Servers { get; }
    public SettingsViewModel Settings { get; }
    public MeshnetViewModel Meshnet { get; }
    public BreachViewModel Breach { get; }

    public EngineClient Engine => _engine;

    public MainViewModel()
    {
        Settings = new SettingsViewModel(_engine);
        Servers = new ServersViewModel(_engine, () => Status.Node);
        Meshnet = new MeshnetViewModel(_engine);
        Breach = new BreachViewModel(_engine);

        Servers.ConnectRequested += (_, node) => _ = ConnectToAsync(node);
        Settings.ProfileImported += (_, path) => _ = ConnectProfileAsync(path);

        _startupSettings = SettingsStore.Load();
        _pollTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(1) };
        _pollTimer.Tick += async (_, _) => await PollAsync().ConfigureAwait(true);

        ConnectCommand = new RelayCommand(
            () => ConnectAsync(),
            () => Status.State is "disconnected");
        DisconnectCommand = new RelayCommand(
            () => DisconnectAsync(),
            () => Status.State is "connected" or "connecting");
        QuickConnectCommand = new RelayCommand(
            () => QuickConnectAsync(),
            () => Status.State is "disconnected");

        ShowDashboardCommand = new RelayCommand(ViewModelBase.Do(() => Navigate("dashboard")));
        ShowServersCommand = new RelayCommand(ViewModelBase.Do(() => Navigate("servers")));
        ShowMeshnetCommand = new RelayCommand(ViewModelBase.Do(() => Navigate("meshnet")));
        ShowBreachCommand = new RelayCommand(ViewModelBase.Do(() => Navigate("breach")));
        ShowSettingsCommand = new RelayCommand(ViewModelBase.Do(() => Navigate("settings")));

        _ = StartupAsync();
    }

    // ------------------------------------------------------------- commands -
    public RelayCommand ConnectCommand { get; }
    public RelayCommand DisconnectCommand { get; }
    public RelayCommand QuickConnectCommand { get; }
    public RelayCommand ShowDashboardCommand { get; }
    public RelayCommand ShowServersCommand { get; }
    public RelayCommand ShowMeshnetCommand { get; }
    public RelayCommand ShowBreachCommand { get; }
    public RelayCommand ShowSettingsCommand { get; }

    // --------------------------------------------------------------- state --
    public VpnStatus Status
    {
        get => _status;
        private set
        {
            if (Set(ref _status, value))
            {
                Raise(nameof(IsConnected));
                Raise(nameof(IsBusy));
                Raise(nameof(IsDisconnected));
                Raise(nameof(ButtonText));
                Raise(nameof(StateLine));
                Raise(nameof(IsSecured));
                Raise(nameof(SessionDuration));
                Raise(nameof(HandshakeAge));
            }
        }
    }

    public bool IsConnected => Status.IsConnected;
    public bool IsBusy => Status.IsBusy;
    public bool IsDisconnected => Status.State == "disconnected";
    public bool IsSecured => Status.IsConnected && Status.Hops > 0;
    public bool TransportAlive
    {
        get => _transportAlive;
        private set { if (Set(ref _transportAlive, value)) Raise(nameof(DaemonStatusText)); }
    }

    public string DaemonStatusText => TransportAlive
        ? $"engine v{_daemonVersion ?? "?"} — connected"
        : "engine offline";

    public string ButtonText => Status.State switch
    {
        "connected" => "DISCONNECT",
        "connecting" => "CONNECTING…",
        "disconnecting" => "DISCONNECTING…",
        _ => "CONNECT",
    };

    public string StateLine => Status.State switch
    {
        "connected" => $"Secured via {Status.Node ?? "node"}",
        "connecting" => "Establishing tunnel…",
        "disconnecting" => "Tearing down…",
        _ => "Not protected",
    };

    public string DownRate
    {
        get => _downRate;
        private set => Set(ref _downRate, value);
    }

    public string UpRate
    {
        get => _upRate;
        private set => Set(ref _upRate, value);
    }

    public string SessionDuration => _connectedSince is null
        ? "—"
        : (DateTime.UtcNow - _connectedSince.Value) switch
        {
            var d when d.TotalHours >= 1 => $"{(int)d.TotalHours}h {d.Minutes}m",
            var d => $"{d.Minutes}m {d.Seconds}s",
        };

    public string HandshakeAge => Status.HandshakeAt <= 0
        ? "—"
        : DateTime.UtcNow - DateTimeOffset.FromUnixTimeSeconds(Status.HandshakeAt).UtcDateTime switch
        {
            var d when d.TotalSeconds < 5 => "just now",
            var d when d.TotalMinutes < 1 => $"{(int)d.TotalSeconds}s ago",
            var d when d.TotalHours < 1 => $"{(int)d.TotalMinutes}m ago ⚠",
            _ => "stale ⚠",
        };

    // ------------------------------------------------------------ view model -
    public object CurrentView => _currentNav switch
    {
        "servers" => Servers,
        "meshnet" => Meshnet,
        "breach" => Breach,
        "settings" => Settings,
        _ => this, // dashboard is the MainViewModel itself (dashboard card layout)
    };

    public bool IsDashboard => _currentNav == "dashboard";

    private void Navigate(string nav)
    {
        _currentNav = nav;
        Raise(nameof(CurrentView));
        Raise(nameof(IsDashboard));
        if (nav == "servers") _ = Servers.RefreshAsync();
        if (nav == "meshnet") _ = Meshnet.RefreshAsync();
    }

    // ------------------------------------------------------------- connect --
    private async Task ConnectAsync(VpnNode? node = null)
    {
        var target = node ?? Servers.SelectedNode ?? Servers.BestNode();
        if (target is null)
        {
            MessageBox.Show("No nodes available yet — refresh the server list.",
                "AegisVPN", MessageBoxButton.OK, MessageBoxImage.Information);
            return;
        }

        var relay = Servers.SelectedRelay;
        var opts = new ConnectOptions
        {
            Node = target.Id,
            Relay = string.IsNullOrEmpty(relay?.Id) ? null : relay.Id,
            KillSwitch = Settings.KillSwitch,
            ThreatProtection = Settings.ThreatProtection,
            Obfuscate = Settings.Obfuscate,
            ObfsKey = string.IsNullOrWhiteSpace(Settings.ObfsKey) ? null : Settings.ObfsKey,
            SplitExclude = Settings.SplitExclude.Count > 0 ? Settings.SplitExclude.ToList() : null,
            SplitInclude = Settings.SplitInclude.Count > 0 ? Settings.SplitInclude.ToList() : null,
            AutoConnect = Settings.AutoConnect,
        };

        var s = SettingsStore.Load();
        s.LastNodeId = target.Id;
        s.LastRelayId = relay?.Id;
        SettingsStore.Save(s);

        Status = new VpnStatus { State = "connecting" };
        try
        {
            Status = await _engine.ConnectAsync(opts).ConfigureAwait(true);

            // DNS takeover after the tunnel is up (protocol: separate `dns` op)
            var dns = Settings.DnsServers
                .Split(',', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
                .Where(x => x.Length > 0).ToArray();
            if (dns.Length > 0)
            {
                try { await _engine.SetDnsAsync(dns).ConfigureAwait(true); }
                catch (EngineException) { /* non-fatal: node DNS already sane */ }
            }
        }
        catch (EngineException ex)
        {
            Status = new VpnStatus { State = "disconnected", Error = ex.Message };
            MessageBox.Show(ex.Message, "AegisVPN", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
    }

    private Task ConnectToAsync(VpnNode node) => ConnectAsync(node);

    private async Task QuickConnectAsync() => await ConnectAsync(null).ConfigureAwait(true);

    private async Task ConnectProfileAsync(string path)
    {
        Status = new VpnStatus { State = "connecting" };
        try
        {
            Status = await _engine.ConnectAsync(new ConnectOptions { Profile = path }).ConfigureAwait(true);
        }
        catch (EngineException ex)
        {
            Status = new VpnStatus { State = "disconnected", Error = ex.Message };
            MessageBox.Show(ex.Message, "AegisVPN", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
    }

    private async Task DisconnectAsync()
    {
        try
        {
            Status = await _engine.DisconnectAsync().ConfigureAwait(true);
        }
        catch (EngineException ex)
        {
            MessageBox.Show(ex.Message, "AegisVPN", MessageBoxButton.OK, MessageBoxImage.Warning);
        }
    }

    // ---------------------------------------------------------------- boot --
    private async Task StartupAsync()
    {
        try
        {
            await _engine.EnsureDaemonAsync().ConfigureAwait(true);
            _daemonVersion = await _engine.GetVersionAsync().ConfigureAwait(true);
            TransportAlive = true;

            // apply persisted toggles when idle (best-effort)
            try
            {
                await _engine.SetKillSwitchAsync(_startupSettings.KillSwitch).ConfigureAwait(true);
                await _engine.SetThreatProtectionAsync(_startupSettings.ThreatProtection).ConfigureAwait(true);
                if (_startupSettings.SplitExclude.Count > 0 || _startupSettings.SplitInclude.Count > 0)
                    await _engine.SetSplitTunnelAsync(_startupSettings.SplitExclude, _startupSettings.SplitInclude).ConfigureAwait(true);
            }
            catch (EngineException) { /* apply at next connect */ }

            _pollTimer.Start();
            _ = Servers.RefreshAsync();

            if (_startupSettings.AutoConnect)
            {
                Servers.SelectedNode = Servers.Nodes.FirstOrDefault(n => n.Id == _startupSettings.LastNodeId);
                Servers.SelectedRelay = Servers.RelayChoices.FirstOrDefault(n => n.Id == _startupSettings.LastRelayId);
                await ConnectAsync().ConfigureAwait(true);
            }
        }
        catch (EngineException ex)
        {
            TransportAlive = false;
            Status = new VpnStatus { State = "disconnected", Error = ex.Message };
        }
    }

    private async Task PollAsync()
    {
        try
        {
            var s = await _engine.GetStatusAsync().ConfigureAwait(true);

            // compute rates from byte deltas
            var now = Environment.TickCount64;
            if (_lastTick > 0 && now > _lastTick)
            {
                var dtSec = (now - _lastTick) / 1000.0;
                DownRate = FormatRate(s.RxBytes > _lastRx ? s.RxBytes - _lastRx : 0, dtSec);
                UpRate = FormatRate(s.TxBytes > _lastTx ? s.TxBytes - _lastTx : 0, dtSec);
            }
            _lastRx = s.RxBytes;
            _lastTx = s.TxBytes;
            _lastTick = now;

            if (s.IsConnected && _connectedSince is null) _connectedSince = DateTime.UtcNow;
            if (!s.IsConnected) _connectedSince = null;

            var wasAlive = TransportAlive;
            Status = s;
            TransportAlive = true;
            System.Windows.Input.CommandManager.InvalidateRequerySuggested();
            if (!wasAlive) _ = Servers.RefreshAsync();
        }
        catch (EngineException)
        {
            TransportAlive = false;
        }
    }

    private static string FormatRate(ulong bytes, double seconds)
    {
        if (seconds <= 0) return "0 B/s";
        var bps = bytes / seconds;
        return bps switch
        {
            >= 1L << 30 => $"{bps / (1L << 30):0.0} GB/s",
            >= 1L << 20 => $"{bps / (1L << 20):0.0} MB/s",
            >= 1L << 10 => $"{bps / (1L << 10):0.0} KB/s",
            _ => $"{bps:0} B/s",
        };
    }

    public void Dispose()
    {
        _pollTimer.Stop();
        _engine.Dispose();
    }
}
