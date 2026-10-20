using System.Windows;
using System.Windows.Controls;
using AegisVPN.Wpf.ViewModels;

namespace AegisVPN.Wpf.Views;

public partial class SettingsView : UserControl
{
    private bool _syncing;

    public SettingsView() => InitializeComponent();

    private SettingsViewModel? VM => DataContext as SettingsViewModel;

    // keep the PasswordBox in sync with the persisted passphrase
    private void OnObfsKeyChanged(object sender, RoutedEventArgs e)
    {
        if (_syncing || VM is null) return;
        VM.ObfsKey = ObfsKeyBox.Password;
    }

    protected override void OnDataContextChanged(DependencyPropertyChangedEventArgs e)
    {
        base.OnDataContextChanged(e);
        if (VM is not null)
        {
            VM.PropertyChanged += (_, args) =>
            {
                if (args.PropertyName == nameof(SettingsViewModel.ObfsKey))
                {
                    var expected = VM.ObfsKey ?? "";
                    if (ObfsKeyBox.Password != expected)
                    {
                        _syncing = true;
                        ObfsKeyBox.Password = expected;
                        _syncing = false;
                    }
                }
            };
            _syncing = true;
            ObfsKeyBox.Password = VM.ObfsKey ?? "";
            _syncing = false;
        }
    }
}
