// Country helpers: emoji flags + human names for registry country codes
// (mirrors gui/desktop/frontend/src/lib/flags.ts for feature parity).

using System.Collections.Generic;

namespace AegisVPN.Wpf.Models;

public static class Countries
{
    private const int FlagOffset = 0x1F1E6; // regional indicator symbol 'A'

    private static readonly Dictionary<string, string> Names = new()
    {
        ["AE"] = "United Arab Emirates", ["AR"] = "Argentina", ["AT"] = "Austria",
        ["AU"] = "Australia", ["BE"] = "Belgium", ["BR"] = "Brazil", ["CA"] = "Canada",
        ["CH"] = "Switzerland", ["CL"] = "Chile", ["CN"] = "Hong Kong", ["CZ"] = "Czechia",
        ["DE"] = "Germany", ["DK"] = "Denmark", ["ES"] = "Spain", ["FI"] = "Finland",
        ["FR"] = "France", ["GB"] = "United Kingdom", ["GR"] = "Greece", ["HK"] = "Hong Kong",
        ["IE"] = "Ireland", ["IL"] = "Israel", ["IN"] = "India", ["IS"] = "Iceland",
        ["IT"] = "Italy", ["JP"] = "Japan", ["KR"] = "South Korea", ["LU"] = "Luxembourg",
        ["MX"] = "Mexico", ["NL"] = "Netherlands", ["NO"] = "Norway", ["NZ"] = "New Zealand",
        ["PL"] = "Poland", ["PT"] = "Portugal", ["RO"] = "Romania", ["SE"] = "Sweden",
        ["SG"] = "Singapore", ["TR"] = "Turkey", ["UA"] = "Ukraine", ["US"] = "United States",
        ["ZA"] = "South Africa",
    };

    public static string Flag(string? code)
    {
        if (string.IsNullOrWhiteSpace(code) || code.Trim().Length != 2)
        {
            return "🌐";
        }

        var cc = code.Trim().ToUpperInvariant();
        if (cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z')
        {
            return "🌐";
        }

        return char.ConvertFromUtf32(FlagOffset + (cc[0] - 'A'))
             + char.ConvertFromUtf32(FlagOffset + (cc[1] - 'A'));
    }

    public static string Name(string? code)
    {
        if (string.IsNullOrWhiteSpace(code))
        {
            return "Unknown";
        }

        var cc = code.Trim().ToUpperInvariant();
        return Names.TryGetValue(cc, out var name) ? name : cc;
    }
}
