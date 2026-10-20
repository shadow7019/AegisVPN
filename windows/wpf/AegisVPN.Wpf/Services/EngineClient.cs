using System.IO;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using AegisVPN.Wpf.Models;

namespace AegisVPN.Wpf.Services;

/// <summary>
/// Client for the AegisVPN engine control daemon.
/// Transport: newline-delimited JSON over TCP 127.0.0.1:5599 (Windows) —
/// the exact contract of docs/PROTOCOL.md §2, implemented by
/// engine/internal/api. One persistent connection, auto-reconnect,
/// requests serialized (the protocol is strict request/response).
/// </summary>
public sealed class EngineClient : IDisposable
{
    public const int ControlPort = 5599;
    private static readonly TimeSpan RequestTimeout = TimeSpan.FromSeconds(15);

    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        PropertyNameCaseInsensitive = true,
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
    };

    private readonly SemaphoreSlim _gate = new(1, 1);
    private TcpClient? _client;
    private StreamReader? _reader;
    private StreamWriter? _writer;

    private string? _engineExePath;
    private bool _disposed;

    /// <summary>Raised whenever the transport becomes available or unavailable.</summary>
    public event EventHandler<bool>? TransportChanged;

    public bool IsTransportAlive
    {
        get
        {
            try { return _client?.Connected == true && _client.Client.Poll(0, SelectMode.SelectWrite); }
            catch { return false; }
        }
    }

    // ------------------------------------------------------------ protocol --

    private record Envelope(bool Ok, JsonElement Data, string? Error);

    /// <summary>Sends one op and deserializes the data payload.</summary>
    public async Task<T?> RequestAsync<T>(string op, object? args = null, CancellationToken ct = default)
    {
        var (ok, data, error) = await RawRequestAsync(op, args, ct).ConfigureAwait(false);
        if (!ok) throw new EngineException(error ?? "engine request failed");
        if (data.ValueKind is JsonValueKind.Null or JsonValueKind.Undefined) return default;
        return data.Deserialize<T>(JsonOpts);
    }

    /// <summary>Fire-and-forget flavor for ops whose payload is ignored.</summary>
    public async Task RequestAsync(string op, object? args = null, CancellationToken ct = default)
    {
        var (ok, _, error) = await RawRequestAsync(op, args, ct).ConfigureAwait(false);
        if (!ok) throw new EngineException(error ?? "engine request failed");
    }

    private async Task<(bool Ok, JsonElement Data, string? Error)> RawRequestAsync(
        string op, object? args, CancellationToken ct)
    {
        await _gate.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            // one transparent reconnect: a stale keep-alive connection is
            // dropped and the request retried exactly once before erroring.
            for (var attempt = 0; ; attempt++)
            {
                try
                {
                    return await RawAttemptAsync(op, args, ct).ConfigureAwait(false);
                }
                catch (Exception ex) when (
                    attempt == 0 &&
                    ex is IOException or SocketException)
                {
                    DropConnection();
                }
            }
        }
        finally
        {
            _gate.Release();
        }
    }

    private async Task<(bool Ok, JsonElement Data, string? Error)> RawAttemptAsync(
        string op, object? args, CancellationToken ct)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(ct);
        timeout.CancelAfter(RequestTimeout);

        var stream = await EnsureConnectedAsync(timeout.Token).ConfigureAwait(false);

        var payload = JsonSerializer.Serialize(new { op, args }, JsonOpts);
        await _writer!.WriteLineAsync(payload).ConfigureAwait(false);
        await _writer.FlushAsync().ConfigureAwait(false);

        var line = await _reader!.ReadLineAsync(timeout.Token).ConfigureAwait(false);
        if (line is null)
        {
            DropConnection();
            throw new IOException("engine closed the connection");
        }

        var env = JsonSerializer.Deserialize<Envelope>(line, JsonOpts);
        if (env is null) throw new EngineException("malformed response from engine");
        return (env.Ok, env.Data, env.Error);
    }

    // ------------------------------------------------------ typed helpers --

    public async Task<VpnStatus> GetStatusAsync(CancellationToken ct = default)
    {
        var s = await RequestAsync<VpnStatus>("status", null, ct).ConfigureAwait(false);
        return s ?? new VpnStatus { State = "disconnected", Error = "empty status payload" };
    }

    public async Task<VpnStatus> ConnectAsync(ConnectOptions opts, CancellationToken ct = default)
    {
        var s = await RequestAsync<VpnStatus>("connect", opts, ct).ConfigureAwait(false);
        return s ?? new VpnStatus { State = "disconnected", Error = "empty status payload" };
    }

    public async Task<VpnStatus> DisconnectAsync(CancellationToken ct = default)
    {
        var s = await RequestAsync<VpnStatus>("disconnect", null, ct).ConfigureAwait(false);
        return s ?? new VpnStatus { State = "disconnected", Error = "empty status payload" };
    }

    public Task SetKillSwitchAsync(bool enabled, CancellationToken ct = default) =>
        RequestAsync("killswitch", new { enabled }, ct);

    public Task SetThreatProtectionAsync(bool enabled, CancellationToken ct = default) =>
        RequestAsync("threat_protection", new { enabled }, ct);

    public Task SetSplitTunnelAsync(IEnumerable<string> exclude, IEnumerable<string> include, CancellationToken ct = default) =>
        RequestAsync("split_tunnel", new { exclude, include }, ct);

    public Task SetDnsAsync(IEnumerable<string> servers, CancellationToken ct = default) =>
        RequestAsync("dns", new { servers }, ct);

    public async Task<List<VpnNode>> GetNodesAsync(CancellationToken ct = default)
    {
        var list = await RequestAsync<List<VpnNode>>("nodes", null, ct).ConfigureAwait(false);
        return list ?? new List<VpnNode>();
    }

    public async Task<List<BreachInfo>> BreachCheckAsync(string email, CancellationToken ct = default)
    {
        var list = await RequestAsync<List<BreachInfo>>("breach_check", new { email }, ct).ConfigureAwait(false);
        return list ?? new List<BreachInfo>();
    }

    public async Task<List<MeshPeer>> GetMeshPeersAsync(CancellationToken ct = default)
    {
        var list = await RequestAsync<List<MeshPeer>>("meshnet_peers", null, ct).ConfigureAwait(false);
        return list ?? new List<MeshPeer>();
    }

    public Task<MeshPeer?> RegisterMeshPeerAsync(string deviceName, string pubkey, CancellationToken ct = default) =>
        RequestAsync<MeshPeer>("meshnet_register", new { deviceName, pubkey }, ct);

    public async Task<string?> GetVersionAsync(CancellationToken ct = default)
    {
        var ping = await RequestAsync<Dictionary<string, string>>("ping", null, ct).ConfigureAwait(false);
        return ping is not null && ping.TryGetValue("version", out var v) ? v : null;
    }

    // ------------------------------------------------------------ daemon ----

    /// <summary>
    /// Ensures the engine daemon is reachable; if not, locates and launches
    /// aegisvpn.exe daemon (installed by the service installer or shipped
    /// beside the GUI) and waits for the control port to come up.
    /// </summary>
    public async Task EnsureDaemonAsync(CancellationToken ct = default)
    {
        if (await ProbePortAsync(300, ct).ConfigureAwait(false)) return;

        _engineExePath ??= LocateEngineExe();
        if (_engineExePath is null)
            throw new EngineException(
                "aegisvpn.exe not found. Install the AegisVPN service (windows/service/install-service.ps1) " +
                "or place aegisvpn.exe next to AegisVPN.exe.");

        var psi = new System.Diagnostics.ProcessStartInfo
        {
            FileName = _engineExePath,
            Arguments = $"daemon --root \"{Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData)}\\AegisVPN\"",
            UseShellExecute = false,
            CreateNoWindow = true,
            WindowStyle = System.Diagnostics.ProcessWindowStyle.Hidden,
        };
        using var proc = System.Diagnostics.Process.Start(psi);
        if (proc is null) throw new EngineException("failed to launch the engine daemon");

        var deadline = Environment.TickCount64 + 15_000;
        while (Environment.TickCount64 < deadline)
        {
            ct.ThrowIfCancellationRequested();
            if (await ProbePortAsync(400, ct).ConfigureAwait(false)) return;
        }
        throw new EngineException("engine daemon did not come up within 15s — check the Windows event log");
    }

    private async Task<bool> ProbePortAsync(int timeoutMs, CancellationToken ct)
    {
        try
        {
            using var probe = new TcpClient();
            using var cts = CancellationTokenSource.CreateLinkedTokenSource(ct);
            cts.CancelAfter(timeoutMs);
            await probe.ConnectAsync("127.0.0.1", ControlPort, cts.Token).ConfigureAwait(false);
            return probe.Connected;
        }
        catch
        {
            return false;
        }
    }

    private static string? LocateEngineExe()
    {
        string[] candidates =
        {
            Path.Combine(AppContext.BaseDirectory, "aegisvpn.exe"),
            Path.Combine(AppContext.BaseDirectory, "bin", "aegisvpn.exe"),
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles), "AegisVPN", "aegisvpn.exe"),
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles), "AegisVPN", "bin", "aegisvpn.exe"),
        };
        return candidates.FirstOrDefault(File.Exists);
    }

    // ----------------------------------------------------------- transport --

    private async Task<NetworkStream> EnsureConnectedAsync(CancellationToken ct)
    {
        if (_client is not null && _client.Connected)
        {
            try
            {
                if (_client.Client.Poll(0, SelectMode.SelectError)) throw new SocketException();
                return _client.GetStream();
            }
            catch
            {
                DropConnection();
            }
        }

        var client = new TcpClient();
        await client.ConnectAsync("127.0.0.1", ControlPort, ct).ConfigureAwait(false);
        client.ReceiveTimeout = (int)RequestTimeout.TotalMilliseconds;
        client.SendTimeout = (int)RequestTimeout.TotalMilliseconds;
        client.NoDelay = true;

        _client = client;
        var stream = client.GetStream();
        _reader = new StreamReader(stream, Encoding.UTF8);
        _writer = new StreamWriter(stream, new UTF8Encoding(false)) { AutoFlush = true };
        TransportChanged?.Invoke(this, true);
        return stream;
    }

    private void DropConnection()
    {
        try { _reader?.Dispose(); } catch { /* ignore */ }
        try { _writer?.Dispose(); } catch { /* ignore */ }
        try { _client?.Dispose(); } catch { /* ignore */ }
        _reader = null;
        _writer = null;
        _client = null;
        TransportChanged?.Invoke(this, false);
    }

    public void Dispose()
    {
        if (_disposed) return;
        _disposed = true;
        DropConnection();
        _gate.Dispose();
    }
}

/// <summary>Friendly error surfaced from the engine control channel.</summary>
public sealed class EngineException : Exception
{
    public EngineException(string message) : base(message) { }
}
