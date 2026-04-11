import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
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
