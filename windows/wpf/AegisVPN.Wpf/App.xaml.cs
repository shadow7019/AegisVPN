using System.IO;
using System.Windows;
using System.Windows.Threading;

namespace AegisVPN.Wpf;

/// <summary>App entry: logs unhandled exceptions to %APPDATA%\AegisVPN\gui.log.</summary>
public partial class App : Application
{
    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        DispatcherUnhandledException += OnDispatcherUnhandledException;
    }

    private void OnDispatcherUnhandledException(object sender, DispatcherUnhandledExceptionEventArgs e)
    {
        try
        {
            var dir = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "AegisVPN");
            Directory.CreateDirectory(dir);
            File.AppendAllText(Path.Combine(dir, "gui.log"),
                $"[{DateTimeOffset.Now:O}] {e.Exception}\n\n");
        }
        catch
        {
            // logging must never crash the app
        }

        MessageBox.Show(
            "Unexpected error:\n" + e.Exception.Message,
            "AegisVPN", MessageBoxButton.OK, MessageBoxImage.Error);
        e.Handled = true;
    }
}
