---
name: Frontend Responsive
description: Implement responsive, mobile-first layouts using Tailwind CSS breakpoints for the Kagent web UI. Use this skill when implementing mobile-first responsive designs that start with mobile styles and progressively enhance for larger screens in ui/src/components/, when using Tailwind breakpoints (sm: 640px, md: 768px, lg: 1024px, xl: 1280px, 2xl: 1536px) to adapt layouts for different device sizes, when hiding or showing elements at different breakpoints using visibility utilities (hidden md:block for desktop-only content, block md:hidden for mobile-only content), when implementing responsive navigation patterns with hamburger menus on mobile that toggle visibility and full horizontal navigation on desktop (md:flex, md:hidden classes), when creating responsive grid layouts that adjust column counts (grid-cols-1 md:grid-cols-2 lg:grid-cols-3), when working with responsive spacing that reduces on mobile and increases on desktop (p-4 md:p-6 lg:p-8, space-x-2 lg:space-x-4, gap-4 md:gap-6), when implementing the useIsMobile() hook from ui/src/hooks/use-mobile.tsx for JavaScript-based responsive logic that checks if window width is below 768px, when ensuring touch targets are at least 44x44px (h-11 w-11 or px-4 py-2) for mobile users to tap comfortably, when implementing responsive typography that scales with screen size (text-2xl md:text-3xl lg:text-4xl, text-sm md:text-base), when working with responsive dialog or modal layouts that adapt button layouts (flex-1 sm:flex-initial for mobile full-width buttons), when implementing responsive sidebars that are hidden on mobile (hidden lg:block) and use Sheet/Drawer components for mobile access, when implementing flex direction changes (flex-col md:flex-row) to stack elements vertically on mobile and horizontally on desktop, when creating responsive chat interfaces with input areas that change positioning (bottom-0 md:bottom-2, rounded-none md:rounded-lg), when implementing responsive forms that stack fields in single column on mobile (grid-cols-1 md:grid-cols-2), when optimizing images for responsive sizes with Next.js Image component and sizes attribute, or when testing designs across mobile viewports (375px iPhone SE, 390px iPhone 12), tablet viewports (768px iPad, 1024px iPad Pro), and desktop viewports (1280px, 1440px, 1920px). This skill ensures the Kagent UI works seamlessly on all device sizes by following mobile-first design principles with progressive enhancement.
---

## When to use this skill

- When implementing mobile-first layouts in `ui/src/components/`
- When using Tailwind breakpoints (`sm:`, `md:`, `lg:`, `xl:`, `2xl:`)
- When hiding/showing elements at different sizes (`hidden md:block`, `md:hidden`)
- When implementing responsive navigation (hamburger on mobile, full nav on desktop)
- When creating responsive grids (`grid-cols-1 md:grid-cols-2 lg:grid-cols-3`)
- When adjusting spacing for different screens (`p-4 md:p-6 lg:p-8`)
- When using the `useIsMobile()` hook for JavaScript-based responsive logic
- When ensuring touch targets are adequately sized (44x44px minimum)
- When implementing responsive typography (`text-sm md:text-base lg:text-lg`)
- When working with responsive dialogs or modals (`sm:max-w-md`)
- When implementing flex direction changes (`flex-col md:flex-row`)
- When creating responsive sidebars (`hidden lg:block`)
- When ensuring forms stack properly on mobile and arrange horizontally on desktop

# Frontend Responsive

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle frontend responsive.

## Instructions

For details, refer to the information provided in this file:
[frontend responsive](../../../agent-os/standards/frontend/responsive.md)
