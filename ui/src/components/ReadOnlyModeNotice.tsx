import Link from "next/link";
import { Lock, ArrowLeft } from "lucide-react";

import { Button } from "@/components/ui/button";

interface ReadOnlyModeNoticeProps {
  title: string;
  description: string;
  href: string;
  hrefLabel: string;
}

export function ReadOnlyModeNotice({ title, description, href, hrefLabel }: ReadOnlyModeNoticeProps) {
  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-lg rounded-lg border bg-card p-8 shadow-sm">
        <div className="mb-6 flex h-12 w-12 items-center justify-center rounded-full bg-muted">
          <Lock className="h-5 w-5" />
        </div>
        <h1 className="text-2xl font-semibold">{title}</h1>
        <p className="mt-3 text-sm text-muted-foreground">{description}</p>
        <Button className="mt-6" variant="outline" asChild>
          <Link href={href}>
            <ArrowLeft className="mr-2 h-4 w-4" />
            {hrefLabel}
          </Link>
        </Button>
      </div>
    </div>
  );
}
