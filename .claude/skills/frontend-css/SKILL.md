---
name: Frontend CSS
description: Style React components using Tailwind CSS 3.4.17 utility classes, Class Variance Authority (CVA) for type-safe variants, and HSL-based design tokens for the Kagent web UI. Use this skill when adding styles to TSX components with Tailwind utility classes directly in className attributes (px-4, py-2, bg-primary, text-foreground, rounded-md), when creating component variants using CVA's cva() function with variants and defaultVariants options in ui/src/components/ui/, when working with the semantic HSL color system (bg-primary, text-foreground, border-border, bg-destructive, bg-muted, bg-accent) that automatically adapts to light and dark modes, when implementing dark mode support with the dark: prefix (dark:bg-black, dark:text-white) based on the .dark class on the HTML element, when using the cn() utility function from lib/utils.ts that combines clsx and twMerge to merge classes intelligently and handle conditional classes, when defining responsive styles with Tailwind's mobile-first breakpoints (sm: 640px, md: 768px, lg: 1024px, xl: 1280px, 2xl: 1536px), when working with Tailwind spacing utilities (p-4, m-2, gap-4, space-x-2) based on the 4px spacing scale, when working with typography utilities (text-sm, font-medium, leading-none) using Geist Sans and Geist Mono fonts, when implementing animations with tailwindcss-animate (animate-accordion-down) or transition utilities (transition-colors, transition-all duration-300), when styling form validation error states with border-destructive and text-destructive semantic colors, when implementing hover, focus-visible, or disabled states (hover:bg-primary/90, focus-visible:ring-1, disabled:opacity-50), when working with layout utilities (flex, grid, items-center, justify-between), when styling markdown content with the prose class and @tailwindcss/typography plugin (prose dark:prose-invert), or when avoiding custom CSS in favor of Tailwind utilities. This skill ensures consistent, maintainable, utility-first styling across the application with automatic dark mode support and type-safe component variants.
---

## When to use this skill

- When adding styles to React components using Tailwind utility classes
- When creating component variants with Class Variance Authority (CVA)
- When working with semantic colors (`bg-primary`, `text-foreground`, `border-border`)
- When implementing dark mode with `dark:` prefix utilities
- When using the `cn()` utility from `lib/utils.ts` to merge classes
- When implementing responsive styles with breakpoints (`md:`, `lg:`, `xl:`)
- When working with Tailwind spacing utilities (`p-4`, `m-2`, `gap-4`, `space-x-2`)
- When styling forms with validation states (`border-destructive`, `text-destructive`)
- When implementing hover, focus, or active states
- When using Tailwind typography utilities (`text-sm`, `font-bold`, `leading-none`)
- When working with flexbox or grid layouts
- When implementing animations with `transition-colors`, `animate-*` classes
- When styling markdown content with `prose dark:prose-invert`

# Frontend CSS

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle frontend CSS.

## Instructions

For details, refer to the information provided in this file:
[frontend CSS](../../../agent-os/standards/frontend/css.md)
