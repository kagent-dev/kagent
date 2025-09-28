"use client";
import Link from "next/link";

export function Footer() {
  return (
    <footer className="mt-auto border-t">
      <div className="max-w-6xl mx-auto px-6 py-12">
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-8">
          <div className="space-y-2">
            <div className="text-xl font-semibold tracking-tight">adolphe.ai</div>
            <p className="text-sm text-muted-foreground">
              Open‑source agentic AI platform for building, running, and scaling AI agents.
            </p>
          </div>

          <div>
            <div className="text-sm font-medium mb-3">Product</div>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li><Link className="hover:text-foreground" href="/product">Overview</Link></li>
              <li><Link className="hover:text-foreground" href="/agents">Agents</Link></li>
              <li><Link className="hover:text-foreground" href="/models">Models</Link></li>
              <li><Link className="hover:text-foreground" href="/tools">Tools</Link></li>
              <li><Link className="hover:text-foreground" href="/servers">Tool Servers</Link></li>
            </ul>
          </div>

          <div>
            <div className="text-sm font-medium mb-3">Resources</div>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li><Link className="hover:text-foreground" href="/resources">Overview</Link></li>
              <li><Link className="hover:text-foreground" href="/resources/docs">Docs</Link></li>
              <li><Link className="hover:text-foreground" href="/resources/guides">Guides</Link></li>
              <li><Link className="hover:text-foreground" href="/resources/examples">Examples</Link></li>
            </ul>
          </div>

          <div>
            <div className="text-sm font-medium mb-3">Company</div>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li><Link className="hover:text-foreground" href="/company">Overview</Link></li>
              <li><Link className="hover:text-foreground" href="/company/about">About</Link></li>
              <li><Link className="hover:text-foreground" href="/company/contact">Contact</Link></li>
              <li><Link className="hover:text-foreground" href="/company/status">Status</Link></li>
            </ul>
          </div>
        </div>

        <div className="mt-10 flex flex-col sm:flex-row items-center justify-between gap-4 border-t pt-6 text-xs text-muted-foreground">
          <p>© {new Date().getFullYear()} adolphe.ai. All rights reserved.</p>
          <div className="flex items-center gap-4">
            <Link className="hover:text-foreground" href="/">Terms</Link>
            <span className="opacity-50">•</span>
            <Link className="hover:text-foreground" href="/">Privacy</Link>
          </div>
        </div>
      </div>
    </footer>
  );
}
