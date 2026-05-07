import { Suspense } from "react";
import { OpenshellTerminalPage } from "./OpenshellTerminalPage";

export default function OpenshellPage() {
  return (
    <Suspense
      fallback={<div className="mx-auto max-w-7xl px-4 py-8 text-sm text-muted-foreground">Loading…</div>}
    >
      <OpenshellTerminalPage />
    </Suspense>
  );
}
