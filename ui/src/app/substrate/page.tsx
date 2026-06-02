import { Suspense } from "react";
import { SubstrateStatusPage } from "./SubstrateStatusPage";

export default function SubstratePage() {
  return (
    <Suspense
      fallback={
        <div className="mx-auto max-w-6xl px-4 py-8 text-sm text-muted-foreground">Loading substrate status…</div>
      }
    >
      <SubstrateStatusPage />
    </Suspense>
  );
}
