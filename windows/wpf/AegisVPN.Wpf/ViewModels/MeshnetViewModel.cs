using System.Collections.ObjectModel;
using AegisVPN.Wpf.Models;
using AegisVPN.Wpf.Services;

namespace AegisVPN.Wpf.ViewModels;

/// <summary>Meshnet: list peers, register this device, refresh.</summary>
public sealed class MeshnetViewModel : ViewModelBase
{
    private bool _isBusy;
    private string _deviceName = Environment.MachineName;

    public ObservableCollection<MeshPeer> Peers { get; } = new();

    public string DeviceName
    {
        get => _deviceName;
        set => Set(ref _deviceName, value);
    }

    public bool IsBusy
    {
        get => _isBusy;
        private set => Set(ref _isBusy, value);
    }

    public RelayCommand RefreshCommand { get; }
    public RelayCommand RegisterCommand { get; }

    private readonly EngineClient _engine;

    public MeshnetViewModel(EngineClient engine)
    {
        _engine = engine;
        RefreshCommand = new RelayCommand(() => RefreshAsync());
        RegisterCommand = new RelayCommand(() => RegisterAsync());
    }

    public async Task RefreshAsync()
    {
        if (IsBusy) return;
        IsBusy = true;
        try
        {
            var peers = await _engine.GetMeshPeersAsync().ConfigureAwait(true);
            Peers.Clear();
            foreach (var p in peers) Peers.Add(p);
        }
        catch (EngineException)
        {
            // surfaced by main status
        }
        finally
        {
            IsBusy = false;
        }
    }

    private async Task RegisterAsync()
    {
        if (IsBusy) return;
        IsBusy = true;
        try
        {
            await _engine.RegisterMeshPeerAsync(DeviceName, "").ConfigureAwait(true);
            await RefreshAsync().ConfigureAwait(true);
        }
        finally
        {
            IsBusy = false;
        }
    }
}
