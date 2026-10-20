/** Country helpers: emoji flags + human names for registry country codes. */

const FLAG_OFFSET = 0x1f1e6; // regional indicator symbol 'A'

export function countryFlag(code: string): string {
  if (!/^[A-Za-z]{2}$/.test(code)) return "🌐";
  const cc = code.toUpperCase();
  return String.fromCodePoint(
    FLAG_OFFSET + (cc.charCodeAt(0) - 65),
    FLAG_OFFSET + (cc.charCodeAt(1) - 65),
  );
}

const COUNTRY_NAMES: Record<string, string> = {
  AE: "United Arab Emirates", AR: "Argentina", AT: "Austria", AU: "Australia",
  BE: "Belgium", BR: "Brazil", CA: "Canada", CH: "Switzerland", CL: "Chile",
  CN: "Hong Kong", CZ: "Czechia", DE: "Germany", DK: "Denmark", ES: "Spain",
  FI: "Finland", FR: "France", GB: "United Kingdom", GR: "Greece", HK: "Hong Kong",
  IE: "Ireland", IL: "Israel", IN: "India", IS: "Iceland", IT: "Italy",
  JP: "Japan", KR: "South Korea", LU: "Luxembourg", MX: "Mexico", NL: "Netherlands",
  NO: "Norway", NZ: "New Zealand", PL: "Poland", PT: "Portugal", RO: "Romania",
  SE: "Sweden", SG: "Singapore", TR: "Turkey", UA: "Ukraine", US: "United States",
  ZA: "South Africa",
};

export function countryName(code: string): string {
  return COUNTRY_NAMES[code.toUpperCase()] ?? code.toUpperCase();
}
