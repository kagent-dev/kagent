import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  logging: {
    fetches: {
      fullUrl: true,
    },
  },
  serverRuntimeConfig: {
    trustProxy: true,
  },
  experimental: { swcPlugins: [] },
  compiler: { removeConsole: true },
};

export default nextConfig;
