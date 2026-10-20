using System.Collections.ObjectModel;
using AegisVPN.Wpf.Services;

namespace AegisVPN.Wpf.ViewModels;

/// <summary>Settings: kill switch, Threat Protection, obfuscation, split
/// tunneling, DNS, auto-connect. Persisted to %APPDATA%\AegisVPN.</summary>
public sealed class SettingsViewModel : ViewModelBase
{
    private readonly EngineClient _engine;
    private bool _suppressPersist;

    private bool _killSwitch;
    private bool _threatProtection;
    private bool _obfuscate;
    private string _obfsKey = "";
    private bool _autoConnect;
    private string _dnsServers = "10.8.0.1";
    private string _newExclude = "";
    private string _newInclude = "";

    public ObservableCollection<string> SplitExclude { get; } = new();
    public ObservableCollection<string> SplitInclude { get; } = new();

    public RelayCommand AddExcludeCommand { get; }
    public RelayCommand RemoveExcludeCommand { get; }
    public RelayCommand AddIncludeCommand { get; }
    public RelayCommand RemoveIncludeCommand { get; }
    public RelayCommand ImportProfileCommand { get; }

    public event EventHandler<string>? ProfileImported;

    public bool KillSwitch
    {
        get => _killSwitch;
        set { if (Set(ref _killSwitch, value)) PersistAndApply(s => s.KillSwitch = value,
            () => _engine.SetKillSwitchAsync(value)); }
    }

    public bool ThreatProtection
    {
        get => _threatProtection;
        set { if (Set(ref _threatProtection, value)) PersistAndApply(s => s.ThreatProtection = value,
            () => _engine.SetThreatProtectionAsync(value)); }
    }

    public bool Obfuscate
    {
        get => _obfuscate;
        set { if (Set(ref _obfuscate, value)) Persist(s => s.Obfuscate = value); }
    }

    public string ObfsKey
    {
        get => _obfsKey;
        set { if (Set(ref _obfsKey, value)) Persist(s => s.ObfsKey = value); }
    }

    public bool AutoConnect
    {
        get => _autoConnect;
        set { if (Set(ref _autoConnect, value)) Persist(s => s.AutoConnect = value); }
    }

    public string DnsServers
    {
        get => _dnsServers;
        set { if (Set(ref _dnsServers, value)) Persist(s => s.DnsServers = value); }
    }

    public string NewExclude
    {
        get => _newExclude;
        set => Set(ref _newExclude, value);
    }

    public string NewInclude
    {
        get => _newInclude;
        set => Set(ref _newInclude, value);
    }

    public SettingsViewModel(EngineClient engine)
    {
        _engine = engine;
        AddExcludeCommand = new RelayCommand(() =>
        {
            if (System.Net.IPAddress.TryParse(_newExclude.Trim().Split('/')[0], out _)
                || _newExclude.Trim().EndsWith("/0"))
            {
                SplitExclude.Add(_newExclude.Trim());
                NewExclude = "";
                PersistSplit();
            }
            return Task.CompletedTask;
        });
        RemoveExcludeCommand = new RelayCommand(param =>
        {
            if (param is string s && SplitExclude.Remove(s)) PersistSplit();
            return Task.CompletedTask;
        });
        AddIncludeCommand = new RelayCommand(() =>
        {
            if (System.Net.IPAddress.TryParse(_newInclude.Trim().Split('/')[0], out _)
                || _newInclude.Trim().EndsWith("/0"))
            {
                SplitInclude.Add(_newInclude.Trim());
                NewInclude = "";
                PersistSplit();
            }
            return Task.CompletedTask;
        });
        RemoveIncludeCommand = new RelayCommand(param =>
        {
            if (param is string s && SplitInclude.Remove(s)) PersistSplit();
            return Task.CompletedTask;
        });
        ImportProfileCommand = new RelayCommand(async () =>
        {
            var dlg = new Microsoft.Win32.OpenFileDialog
            {
                Title = "Import wg-quick profile",
                Filter = "WireGuard profiles (*.conf)|*.conf|All files (*.*)|*.*",
            };
            if (dlg.ShowDialog() == true)
                ProfileImported?.Invoke(this, dlg.FileName);
            await Task.CompletedTask;
        });

        Load();
    }

    private void Load()
    {
        var s = SettingsStore.Load();
        _suppressPersist = true;
        KillSwitch = s.KillSwitch;
        ThreatProtection = s.ThreatProtection;
        Obfuscate = s.Obfuscate;
        ObfsKey = s.ObfsKey;
        AutoConnect = s.AutoConnect;
        DnsServers = s.DnsServers;
        SplitExclude.Clear();
        foreach (var x in s.SplitExclude) SplitExclude.Add(x);
        SplitInclude.Clear();
        foreach (var x in s.SplitInclude) SplitInclude.Add(x);
        _suppressPersist = false;
    }

    public AppSettings Snapshot() => new()
    {
        KillSwitch = KillSwitch,
        ThreatProtection = ThreatProtection,
        Obfuscate = Obfuscate,
        ObfsKey = ObfsKey,
        AutoConnect = AutoConnect,
        DnsServers = DnsServers,
        SplitExclude = SplitExclude.ToList(),
        SplitInclude = SplitInclude.ToList(),
    };

    private void PersistSplit() => Persist(s =>
    {
        s.SplitExclude = SplitExclude.ToList();
        s.SplitInclude = SplitInclude.ToList();
    });

    private void Persist(Action<AppSettings> mutate)
    {
        if (_suppressPersist) return;
        var s = Snapshot();
        mutate(s);
        SettingsStore.Save(s);
    }

    private void PersistAndApply(Action<AppSettings> mutate, Func<Task> engineCall)
    {
        Persist(mutate);
        if (_suppressPersist) return;
        _ = Task.Run(async () =>
        {
            try { await engineCall(); }
            catch (EngineException) { /* daemon may be down; applied at next connect */ }
        });
    }
}
