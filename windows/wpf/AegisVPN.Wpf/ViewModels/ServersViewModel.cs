using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Windows.Data;
using AegisVPN.Wpf.Models;
using AegisVPN.Wpf.Services;

namespace AegisVPN.Wpf.ViewModels;

/// <summary>Server browser: search, grouping by country, load/latency badges,
/// Double VPN relay selection.</summary>
public sealed class ServersViewModel : ViewModelBase
{
    private string _search = "";
    private VpnNode? _selectedNode;
    private VpnNode? _selectedRelay;
    private bool _isRefreshing;

    public event EventHandler<VpnNode>? ConnectRequested;

    public ObservableCollection<VpnNode> Nodes { get; } = new();
    public ObservableCollection<VpnNode> RelayChoices { get; } = new();

    /// <summary>Grouped + filtered view of Nodes (bind the ListBox here).</summary>
    public ICollectionView NodesView { get; }

    public string Search
    {
        get => _search;
        set { if (Set(ref _search, value)) NodesView.Refresh(); }
    }

    public VpnNode? SelectedNode
    {
        get => _selectedNode;
        set => Set(ref _selectedNode, value);
    }

    public VpnNode? SelectedRelay
    {
        get => _selectedRelay;
        set => Set(ref _selectedRelay, value);
    }

    public bool IsRefreshing
    {
        get => _isRefreshing;
        private set => Set(ref _isRefreshing, value);
    }

    public RelayCommand ConnectCommand { get; }
    public RelayCommand RefreshCommand { get; }

    private readonly EngineClient _engine;
    private readonly Func<string?> _activeNodeAccessor;

    public ServersViewModel(EngineClient engine, Func<string?> activeNodeAccessor)
    {
        _engine = engine;
        _activeNodeAccessor = activeNodeAccessor;
        ConnectCommand = new RelayCommand(
            param => { if (param is VpnNode n) ConnectRequested?.Invoke(this, n); return Task.CompletedTask; });
        RefreshCommand = new RelayCommand(() => RefreshAsync());

        NodesView = CollectionViewSource.GetDefaultView(Nodes);
        NodesView.Filter = FilterNode;
        NodesView.GroupDescriptions.Add(new PropertyGroupDescription(nameof(VpnNode.Country)));
    }

    private bool FilterNode(object item)
    {
        if (item is not VpnNode n) return false;
        var q = Search?.Trim() ?? "";
        if (q.Length == 0) return true;
        return n.Country.Contains(q, StringComparison.OrdinalIgnoreCase)
            || n.City.Contains(q, StringComparison.OrdinalIgnoreCase)
            || n.Id.Contains(q, StringComparison.OrdinalIgnoreCase)
            || n.Type.Contains(q, StringComparison.OrdinalIgnoreCase);
    }

    public async Task RefreshAsync(bool force = true)
    {
        if (IsRefreshing) return;
        IsRefreshing = true;
        try
        {
            var nodes = await _engine.GetNodesAsync().ConfigureAwait(true);
            Nodes.Clear();
            foreach (var n in nodes.OrderBy(n => n.Country).ThenBy(n => n.City)) Nodes.Add(n);

            RelayChoices.Clear();
            RelayChoices.Add(new VpnNode { Id = "", Country = "", City = "None (single hop)", Type = "standard" });
            foreach (var n in nodes.Where(n => n.Type is "standard" or "double" or "p2p"))
                RelayChoices.Add(n);
        }
        catch (EngineException)
        {
            // transport errors surface through the main status; keep list as-is
        }
        finally
        {
            IsRefreshing = false;
        }
    }

    /// <summary>Pick the best node: healthy load, best latency.</summary>
    public VpnNode? BestNode()
    {
        VpnNode? best = null;
        foreach (var n in Nodes)
        {
            if (n.Load >= 0.75) continue;
            if (best is null || n.LatencyMs < best.LatencyMs) best = n;
        }
        return best ?? Nodes.FirstOrDefault();
    }
}
