using System.Globalization;
using System.Windows;
using System.Windows.Data;
using System.Windows.Media;

namespace AegisVPN.Wpf.Converters;

/// <summary>state string → status brush (green connected / amber busy / gray idle).</summary>
public sealed class StateToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        var state = value as string ?? "disconnected";
        return state switch
        {
            "connected" => new SolidColorBrush(Color.FromRgb(0x22, 0xC5, 0x5E)),
            "connecting" or "disconnecting" => new SolidColorBrush(Color.FromRgb(0xF5, 0x9E, 0x0B)),
            _ => new SolidColorBrush(Color.FromRgb(0x6B, 0x72, 0x80)),
        };
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}

/// <summary>null/empty string → Collapsed, else Visible.</summary>
public sealed class StringToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        string.IsNullOrWhiteSpace(value as string) ? Visibility.Collapsed : Visibility.Visible;

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}

/// <summary>null → Collapsed, else Visible.</summary>
public sealed class NullToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        value is null ? Visibility.Collapsed : Visibility.Visible;

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}

/// <summary>true → Collapsed (inverse of BooleanToVisibilityConverter).</summary>
public sealed class InverseBoolToVisibilityConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        value is true ? Visibility.Collapsed : Visibility.Visible;

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}

/// <summary>load 0..1 → green/yellow/red brush for load bars.</summary>
public sealed class LoadToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        var load = value is double d ? d : 0;
        if (load < 0.5) return new SolidColorBrush(Color.FromRgb(0x22, 0xC5, 0x5E));
        if (load < 0.75) return new SolidColorBrush(Color.FromRgb(0xF5, 0x9E, 0x0B));
        return new SolidColorBrush(Color.FromRgb(0xEF, 0x44, 0x44));
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}

/// <summary>int latency ms → colored text brush (fast green / ok amber / slow red).</summary>
public sealed class LatencyToBrushConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        var ms = value is int i ? i : int.MaxValue;
        if (ms < 100) return new SolidColorBrush(Color.FromRgb(0x22, 0xC5, 0x5E));
        if (ms < 250) return new SolidColorBrush(Color.FromRgb(0xF5, 0x9E, 0x0B));
        return new SolidColorBrush(Color.FromRgb(0xEF, 0x44, 0x44));
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}

/// <summary>long pwn count → "12.4M accounts" text.</summary>
public sealed class PwnCountConverter : IValueConverter
{
    public object Convert(object? value, Type targetType, object? parameter, CultureInfo culture)
    {
        if (value is not long n) return "";
        return n switch
        {
            >= 1_000_000_000 => $"{n / 1_000_000_000.0:0.0}B accounts",
            >= 1_000_000 => $"{n / 1_000_000.0:0.0}M accounts",
            >= 1_000 => $"{n / 1_000.0:0.0}K accounts",
            _ => $"{n} accounts",
        };
    }

    public object ConvertBack(object? value, Type targetType, object? parameter, CultureInfo culture) =>
        Binding.DoNothing;
}
