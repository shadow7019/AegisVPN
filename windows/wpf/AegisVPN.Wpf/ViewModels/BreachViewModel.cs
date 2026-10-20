using System.Collections.ObjectModel;
using AegisVPN.Wpf.Models;
using AegisVPN.Wpf.Services;

namespace AegisVPN.Wpf.ViewModels;

/// <summary>Dark Web Monitor — HIBP breach lookup via the engine.</summary>
public sealed class BreachViewModel : ViewModelBase
{
    private string _email = "";
    private bool _isChecking;
    private bool _hasSearched;

    public ObservableCollection<BreachInfo> Breaches { get; } = new();

    public string Email
    {
        get => _email;
        set => Set(ref _email, value);
    }

    public bool IsChecking
    {
        get => _isChecking;
        private set => Set(ref _isChecking, value);
    }

    public bool HasSearched
    {
        get => _hasSearched;
        private set => Set(ref _hasSearched, value);
    }

    public string Summary
    {
        get
        {
            if (!HasSearched) return "Check if your email appeared in a known data breach.";
            if (IsChecking) return "Checking breach databases…";
            return Breaches.Count == 0
                ? "✔ No known breaches for this address."
                : $"⚠ {Breaches.Count} breach(es) found for this address.";
        }
    }

    public RelayCommand CheckCommand { get; }

    private readonly EngineClient _engine;

    public BreachViewModel(EngineClient engine)
    {
        _engine = engine;
        CheckCommand = new RelayCommand(() => CheckAsync());
    }

    public async Task CheckAsync()
    {
        if (IsChecking) return;
        var email = Email?.Trim() ?? "";
        if (email.Length == 0 || !email.Contains('@'))
        {
            System.Windows.MessageBox.Show("Enter a valid email address.", "AegisVPN",
                System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
            return;
        }

        IsChecking = true;
        HasSearched = true;
        Raise(nameof(Summary));
        try
        {
            var breaches = await _engine.BreachCheckAsync(email).ConfigureAwait(true);
            Breaches.Clear();
            foreach (var b in breaches) Breaches.Add(b);
        }
        catch (EngineException)
        {
            // surfaced by main status
        }
        finally
        {
            IsChecking = false;
            Raise(nameof(Summary));
        }
    }
}
