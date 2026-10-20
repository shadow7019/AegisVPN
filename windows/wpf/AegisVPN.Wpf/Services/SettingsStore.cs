using System.IO;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace AegisVPN.Wpf.Services;

/// <summary>Persisted GUI settings (%APPDATA%\AegisVPN\settings.json).</summary>
public sealed class AppSettings
{
    [JsonPropertyName("killSwitch")]
    public bool KillSwitch { get; set; } = true;

    [JsonPropertyName("threatProtection")]
    public bool ThreatProtection { get; set; } = true;

    [JsonPropertyName("obfuscate")]
    public bool Obfuscate { get; set; }

    [JsonPropertyName("obfsKey")]
    public string ObfsKey { get; set; } = "";

    [JsonPropertyName("autoConnect")]
    public bool AutoConnect { get; set; }

    [JsonPropertyName("dnsServers")]
    public string DnsServers { get; set; } = "10.8.0.1";

    [JsonPropertyName("splitExclude")]
    public List<string> SplitExclude { get; set; } = new();

    [JsonPropertyName("splitInclude")]
    public List<string> SplitInclude { get; set; } = new();

    [JsonPropertyName("lastNodeId")]
    public string? LastNodeId { get; set; }

    [JsonPropertyName("lastRelayId")]
    public string? LastRelayId { get; set; }
}

public static class SettingsStore
{
    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        WriteIndented = true,
        PropertyNameCaseInsensitive = true,
    };

    public static string Dir { get; } = Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "AegisVPN");

    private static string File => Path.Combine(Dir, "settings.json");

    public static AppSettings Load()
    {
        try
        {
            if (System.IO.File.Exists(File))
            {
                return JsonSerializer.Deserialize<AppSettings>(
                    System.IO.File.ReadAllText(File), JsonOpts) ?? new AppSettings();
            }
        }
        catch
        {
            // corrupted settings → defaults
        }
        return new AppSettings();
    }

    public static void Save(AppSettings settings)
    {
        try
        {
            Directory.CreateDirectory(Dir);
            System.IO.File.WriteAllText(File, JsonSerializer.Serialize(settings, JsonOpts));
        }
        catch
        {
            // best-effort persistence
        }
    }
}
