"use client";

import * as React from "react";
import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";

import { Button } from "@/components/ui/button";

/**
 * Tri-state theme control: one click advances system -> light -> dark -> system.
 *
 * The provider in `app/layout.tsx` is configured with `defaultTheme="system"
 * enableSystem`, but the control used to offer only "light" and "dark". Since
 * next-themes persists the selection, picking either one left no way back to
 * following the OS setting.
 *
 * The accessible name reports the *current* selection rather than a generic
 * "toggle theme", so the three states are distinguishable without seeing the
 * icon.
 */

export const THEME_ORDER = ["system", "light", "dark"] as const;
export type ThemeChoice = (typeof THEME_ORDER)[number];

const ICONS: Record<ThemeChoice, React.ComponentType<{ className?: string }>> = {
  system: Monitor,
  light: Sun,
  dark: Moon,
};

const LABELS: Record<ThemeChoice, string> = {
  system: "Theme: system",
  light: "Theme: light",
  dark: "Theme: dark",
};

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);

  // `theme` is undefined until the client reads the persisted value, so the
  // icon is only meaningful after mount. Render a placeholder until then to
  // keep the server and client markup identical.
  React.useEffect(() => setMounted(true), []);

  const current: ThemeChoice = THEME_ORDER.includes(theme as ThemeChoice)
    ? (theme as ThemeChoice)
    : "system";
  const next = THEME_ORDER[(THEME_ORDER.indexOf(current) + 1) % THEME_ORDER.length];
  const Icon = ICONS[current];

  if (!mounted) {
    return (
      <Button variant="outline" size="icon" disabled aria-hidden>
        <Monitor className="h-[1.2rem] w-[1.2rem] opacity-0" />
      </Button>
    );
  }

  return (
    <Button
      variant="outline"
      size="icon"
      onClick={() => setTheme(next)}
      aria-label={LABELS[current]}
      title={`${LABELS[current]} — click for ${next}`}
    >
      <Icon className="h-[1.2rem] w-[1.2rem]" />
    </Button>
  );
}
