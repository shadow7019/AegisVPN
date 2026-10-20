using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Interop;
using AegisVPN.Wpf.ViewModels;

namespace AegisVPN.Wpf.Views;

public partial class MainWindow : Window
{
    private readonly MainViewModel _vm;

    public MainWindow()
    {
        InitializeComponent();
        _vm = new MainViewModel();
        DataContext = _vm;
        SourceInitialized += (_, _) => FixMaximizedBounds();
        Closed += (_, _) => _vm.Dispose();
    }

    // ── window chrome buttons ────────────────────────────────────────────────
    private void OnMinimize(object sender, RoutedEventArgs e) => WindowState = WindowState.Minimized;

    private void OnMaximize(object sender, RoutedEventArgs e) =>
        WindowState = WindowState == WindowState.Maximized ? WindowState.Normal : WindowState.Maximized;

    private void OnClose(object sender, RoutedEventArgs e) => Close();

    // ── keep a borderless maximized window inside the work area ─────────────
    private void FixMaximizedBounds()
    {
        var source = (HwndSource?)PresentationSource.FromVisual(this);
        source?.AddHook(WndProc);
    }

    private static IntPtr WndProc(IntPtr hwnd, int msg, IntPtr wParam, IntPtr lParam, ref bool handled)
    {
        const int WM_GETMINMAXINFO = 0x0024;
        if (msg != WM_GETMINMAXINFO) return IntPtr.Zero;
        handled = true;

        var mmi = Marshal.PtrToStructure(lParam, typeof(MINMAXINFO)) as MINMAXINFO;
        if (mmi is null) return IntPtr.Zero;

        var monitor = MonitorFromWindow(hwnd, MONITOR_DEFAULTTONEAREST);
        if (monitor != IntPtr.Zero)
        {
            var info = new MONITORINFO();
            info.cbSize = Marshal.SizeOf(info);
            if (GetMonitorInfo(monitor, ref info))
            {
                mmi.ptMaxPosition.x = Math.Abs(info.rcWork.left - info.rcMonitor.left);
                mmi.ptMaxPosition.y = Math.Abs(info.rcWork.top - info.rcMonitor.top);
                mmi.ptMaxSize.x = Math.Abs(info.rcWork.right - info.rcWork.left);
                mmi.ptMaxSize.y = Math.Abs(info.rcWork.bottom - info.rcWork.top);
            }
        }
        Marshal.StructureToPtr(mmi, lParam, true);
        return IntPtr.Zero;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct POINT { public int x; public int y; }

    [StructLayout(LayoutKind.Sequential)]
    private struct MINMAXINFO
    {
        public POINT ptReserved;
        public POINT ptMaxSize;
        public POINT ptMaxPosition;
        public POINT ptMinTrackSize;
        public POINT ptMaxTrackSize;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct RECT { public int left, top, right, bottom; }

    [StructLayout(LayoutKind.Sequential)]
    private struct MONITORINFO
    {
        public int cbSize;
        public RECT rcMonitor;
        public RECT rcWork;
        public uint dwFlags;
    }

    private const int MONITOR_DEFAULTTONEAREST = 2;

    [DllImport("user32.dll")]
    private static extern IntPtr MonitorFromWindow(IntPtr hwnd, uint flags);

    [DllImport("user32.dll")]
    private static extern bool GetMonitorInfo(IntPtr hMonitor, ref MONITORINFO info);
}
