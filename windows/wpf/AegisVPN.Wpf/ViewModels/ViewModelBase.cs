using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Runtime.CompilerServices;

namespace AegisVPN.Wpf.ViewModels;

/// <summary>Minimal INotifyPropertyChanged base — no external MVVM deps.</summary>
public abstract class ViewModelBase : INotifyPropertyChanged
{
    public event PropertyChangedEventHandler? PropertyChanged;

    protected void Raise([CallerMemberName] string? name = null) =>
        PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));

    protected bool Set<T>(ref T field, T value, [CallerMemberName] string? name = null)
    {
        if (EqualityComparer<T>.Default.Equals(field, value)) return false;
        field = value;
        Raise(name);
        return true;
    }

    protected void RaiseAll() =>
        PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(null));

    /// <summary>Adapter so sync work can flow through the async command pipeline.</summary>
    public static Func<Task> Do(Action action) => () =>
    {
        action();
        return Task.CompletedTask;
    };
}

/// <summary>
/// Async-first command: execution goes through Func&lt;object?, Task&gt; so
/// exceptions surface to the user instead of vanishing into unobserved tasks.
/// Re-entrancy guarded.
/// </summary>
public sealed class RelayCommand : System.Windows.Input.ICommand
{
    private readonly Func<object?, Task> _execute;
    private readonly Func<object?, bool>? _canExecute;
    private readonly object _gate = new();
    private bool _running;

    /// <summary>Parameterless async command.</summary>
    public RelayCommand(Func<Task> execute, Func<bool>? canExecute = null)
        : this(_ => execute(), canExecute is null ? null : _ => canExecute()) { }

    /// <summary>Primary constructor.</summary>
    public RelayCommand(Func<object?, Task> execute, Func<object?, bool>? canExecute = null)
    {
        _execute = execute;
        _canExecute = canExecute;
    }

    public bool CanExecute(object? parameter)
    {
        lock (_gate)
        {
            if (_running) return false;
            return _canExecute?.Invoke(parameter) ?? true;
        }
    }

    public async void Execute(object? parameter)
    {
        lock (_gate)
        {
            if (_running) return;
            _running = true;
        }
        try
        {
            await _execute(parameter);
        }
        catch (Exception ex)
        {
            System.Windows.MessageBox.Show(
                ex.Message, "AegisVPN",
                System.Windows.MessageBoxButton.OK,
                System.Windows.MessageBoxImage.Warning);
        }
        finally
        {
            lock (_gate) { _running = false; }
            RaiseCanExecuteChanged();
        }
    }

    public event EventHandler? CanExecuteChanged
    {
        add => System.Windows.Input.CommandManager.RequerySuggested += value;
        remove => System.Windows.Input.CommandManager.RequerySuggested -= value;
    }

    public void RaiseCanExecuteChanged() =>
        System.Windows.Input.CommandManager.InvalidateRequerySuggested();
}
