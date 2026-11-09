import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  /* config options here */
  typescript: {
    ignoreBuildErrors: true,
  },
  reactStrictMode: false,
  async headers() {
    return [
      {
        // Downloadable binaries must never come from a stale browser cache:
        // every rebuild ships new bytes under a new filename, but old links
        // (bookmarks) must also serve fresh bytes, not a cached broken build.
        source: "/downloads/:path*",
        headers: [
          { key: "Cache-Control", value: "no-store, must-revalidate" },
        ],
      },
    ];
  },
};

export default nextConfig;
