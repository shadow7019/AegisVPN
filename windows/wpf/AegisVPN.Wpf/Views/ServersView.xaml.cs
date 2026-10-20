using System.Windows.Controls;
using System.Windows.Input;
using AegisVPN.Wpf.Models;

namespace AegisVPN.Wpf.Views;

public partial class ServersView : UserControl
{
    public ServersView() => InitializeComponent();

    private void OnNodeDoubleClick(object sender, MouseButtonEventArgs e)
    {
        if (DataContext is ViewModels.ServersViewModel vm && vm.SelectedNode is VpnNode node)
            vm.ConnectCommand.Execute(node);
    }
}
