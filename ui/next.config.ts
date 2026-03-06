import type { NextConfig } from "next";

/** Backend root (no /api) for rewrites. Must match getBackendRoot() in src/lib/utils.ts. */
function getBackendRootForRewrites(): string {
  const url =
    process.env.NEXT_PUBLIC_BACKEND_URL ??
    (process.env.NODE_ENV === "production"
      ? "http://kagent.kagent.svc.cluster.local/api"
      : "http://localhost:8083/api");
  return url.replace(/\/api\/?$/, "") || url;
}

const nextConfig: NextConfig = {
  output: "standalone",
  logging: {
    fetches: {
      fullUrl: true,
    },
  },
  experimental: { swcPlugins: [] },
  compiler: { removeConsole: process.env.NODE_ENV === "production" },
  async rewrites() {
    const backendRoot = getBackendRootForRewrites();
    return [
      // Plugin proxy: browser iframe loads /_p/{name}/; forward to Go backend
      // so dev (UI on :8082, backend on :8083) works without nginx.
      { source: "/_p/:path*", destination: `${backendRoot}/_p/:path*` },
    ];
  },
};

export default nextConfig;
