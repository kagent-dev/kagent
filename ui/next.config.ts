import type { NextConfig } from "next";

const controllerDevURL =
  process.env.KAGENT_DEV_CONTROLLER_URL ?? "http://127.0.0.1:8083";

const nextConfig: NextConfig = {
  output: "standalone",
  // Proxy /api to the controller in local dev (next dev :8001 → controller :8083).
  async rewrites() {
    if (process.env.NODE_ENV === "production") {
      return [];
    }
    return [
      {
        source: "/api/:path*",
        destination: `${controllerDevURL}/api/:path*`,
      },
    ];
  },
  logging: {
    fetches: {
      fullUrl: true,
    },
  },
  experimental: { swcPlugins: [] },
  reactCompiler: true,
  compiler: { removeConsole: process.env.NODE_ENV === "production" },
};

export default nextConfig;
