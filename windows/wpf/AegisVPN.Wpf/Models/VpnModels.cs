using System.Text.Json.Serialization;

namespace AegisVPN.Wpf.Models;

/// <summary>Mirror of engine aegis.Status — JSON keys per docs/PROTOCOL.md §1.</summary>
public sealed class VpnStatus
{
    [JsonPropertyName("state")]
    public string State { get; set; } = "disconnected";

    [JsonPropertyName("profile")]
    public string? Profile { get; set; }

    [JsonPropertyName("node")]
    public string? Node { get; set; }

    [JsonPropertyName("localIp")]
    public string? LocalIp { get; set; }

    [JsonPropertyName("endpoint")]
    public string? Endpoint { get; set; }

    [JsonPropertyName("hops")]
    public int Hops { get; set; }

    [JsonPropertyName("killSwitch")]
    public bool KillSwitch { get; set; }

    [JsonPropertyName("threatProtection")]
    public bool ThreatProtection { get; set; }

    [JsonPropertyName("rxBytes")]
    public ulong RxBytes { get; set; }

    [JsonPropertyName("txBytes")]
    public ulong TxBytes { get; set; }

    [JsonPropertyName("handshakeAt")]
    public long HandshakeAt { get; set; }

    [JsonPropertyName("latencyMs")]
    public int LatencyMs { get; set; }

    [JsonPropertyName("error")]
    public string? Error { get; set; }

    public bool IsConnected => State == "connected";
    public bool IsBusy => State is "connecting" or "disconnecting";
}

/// <summary>Mirror of engine aegis.ConnectOpts — JSON keys per docs/PROTOCOL.md §1.</summary>
public sealed class ConnectOptions
{
    [JsonPropertyName("profile")]
    public string? Profile { get; set; }

    [JsonPropertyName("node")]
    public string? Node { get; set; }

    [JsonPropertyName("relay")]
    public string? Relay { get; set; }

    [JsonPropertyName("killSwitch")]
    public bool KillSwitch { get; set; }

    [JsonPropertyName("threatProtection")]
    public bool ThreatProtection { get; set; }

    [JsonPropertyName("splitExclude")]
    public List<string>? SplitExclude { get; set; }

    [JsonPropertyName("splitInclude")]
    public List<string>? SplitInclude { get; set; }

    [JsonPropertyName("obfuscate")]
    public bool Obfuscate { get; set; }

    [JsonPropertyName("obfsKey")]
    public string? ObfsKey { get; set; }

    [JsonPropertyName("autoConnect")]
    public bool AutoConnect { get; set; }
}

/// <summary>Node from the coordination registry (docs/PROTOCOL.md §4).</summary>
public sealed class VpnNode
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("country")]
    public string Country { get; set; } = "";

    [JsonPropertyName("city")]
    public string City { get; set; } = "";

    [JsonPropertyName("host")]
    public string Host { get; set; } = "";

    [JsonPropertyName("port")]
    public int Port { get; set; }

    [JsonPropertyName("pubkey")]
    public string? Pubkey { get; set; }

    [JsonPropertyName("probePort")]
    public int? ProbePort { get; set; }

    [JsonPropertyName("type")]
    public string Type { get; set; } = "standard";

    [JsonPropertyName("load")]
    public double Load { get; set; }

    [JsonPropertyName("latencyMs")]
    public int LatencyMs { get; set; }

    public string CountryFlag
    {
        get
        {
            if (string.IsNullOrEmpty(Country) || Country.Length != 2) return "🌐";
            return string.Concat(Country.ToUpperInvariant().Select(c =>
                char.ConvertFromUtf32(0x1F1E6 + c - 'A')));
        }
    }

    public string TypeBadge => Type switch
    {
        "p2p" => "P2P",
        "double" => "DOUBLE",
        "onion" => "ONION",
        "obfuscated" => "OBFUSCATED",
        "dedicated" => "DEDICATED",
        _ => "STANDARD"
    };

    public string LoadPercent => $"{Math.Clamp(Load * 100, 0, 100):0}%";

    /// <summary>Human label used by ComboBoxes (relay picker) and lists.</summary>
    public string DisplayName => string.IsNullOrEmpty(Id)
        ? (City ?? "None (single hop)")
        : $"{CountryFlag} {Id} — {City}";
}

/// <summary>Meshnet peer (docs/PROTOCOL.md §4 — designed defensively).</summary>
public sealed class MeshPeer
{
    [JsonPropertyName("deviceName")]
    public string? DeviceName { get; set; }

    [JsonPropertyName("pubkey")]
    public string? Pubkey { get; set; }

    [JsonPropertyName("address")]
    public string? Address { get; set; }

    [JsonPropertyName("allowedIps")]
    public List<string>? AllowedIps { get; set; }

    [JsonPropertyName("endpoint")]
    public string? Endpoint { get; set; }

    [JsonPropertyName("online")]
    public bool? Online { get; set; }
}

/// <summary>HIBP breach record (Dark Web Monitor).</summary>
public sealed class BreachInfo
{
    [JsonPropertyName("Name")]
    public string Name { get; set; } = "";

    [JsonPropertyName("Title")]
    public string? Title { get; set; }

    [JsonPropertyName("Domain")]
    public string? Domain { get; set; }

    [JsonPropertyName("BreachDate")]
    public string? BreachDate { get; set; }

    [JsonPropertyName("AddedDate")]
    public string? AddedDate { get; set; }

    [JsonPropertyName("PwnCount")]
    public long PwnCount { get; set; }

    [JsonPropertyName("Description")]
    public string? Description { get; set; }

    [JsonPropertyName("DataClasses")]
    public List<string>? DataClasses { get; set; }

    /// <summary>Fallback-safe title for list rendering.</summary>
    public string DisplayTitle => string.IsNullOrWhiteSpace(Title) ? Name : (Title ?? Name);
}
