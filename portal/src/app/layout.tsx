import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { Toaster } from "@/components/ui/toaster";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:3000"),
  title: "AegisVPN — Fast, Secure VPN for Windows, macOS & Android",
  description:
    "Download AegisVPN: a real WireGuard-based VPN with Kill Switch, Double VPN, Threat Protection, Split Tunneling and an 80-node fleet across 56 countries. Free & open source.",
  keywords: [
    "AegisVPN",
    "VPN download",
    "WireGuard",
    "Kill Switch",
    "Double VPN",
    "Android APK",
    "Windows EXE",
    "macOS DMG",
  ],
  authors: [{ name: "AegisVPN Project" }],
  icons: {
    icon: "/favicon.png",
    apple: "/logo-192.png",
  },
  openGraph: {
    title: "AegisVPN — Shield every connection",
    description:
      "Real WireGuard engine · 80 nodes · 56 countries · Kill Switch · Double VPN · Threat Protection",
    images: ["/logo.png"],
    type: "website",
  },
};

export const viewport: Viewport = {
  themeColor: "#030307",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased bg-background text-foreground`}
      >
        {children}
        <Toaster />
      </body>
    </html>
  );
}
