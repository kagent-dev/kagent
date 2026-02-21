"use client";

import { useEffect, useState } from "react";
import KagentLogo from "./kagent-logo";

export function Footer() {
  const [version, setVersion] = useState(process.env.NEXT_PUBLIC_KAGENT_VERSION || "");

  useEffect(() => {
    if (version) return;

    fetch("/version")
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data?.kagent_version) {
          setVersion(data.kagent_version);
        }
      })
      .catch(() => {
        // Version endpoint not available — leave blank
      });
  }, [version]);

  return (
    <footer className="mt-auto py-5">
      <div className="text-center text-sm text-muted-foreground flex items-center justify-center gap-2">
        <KagentLogo animate={true} className="h-6 w-6 text-[#942DE7]" />
        <p>
          {version ? `v${version} · ` : ""}is an open source project
        </p>
      </div>
    </footer>
  );
}
