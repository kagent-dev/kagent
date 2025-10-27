---
name: Frontend Accessibility
description: Implement accessible user interfaces following WAI-ARIA standards using Radix UI primitives and semantic HTML in the Kagent Next.js frontend. Use this skill when creating or modifying React components in ui/src/components/, when working with Radix UI primitives (Dialog, Dropdown, Select, Checkbox, Tooltip, Tabs, Accordion) that provide automatic WAI-ARIA compliance, when adding ARIA attributes like aria-label, aria-describedby, aria-invalid, aria-expanded, or aria-live to elements, when implementing keyboard navigation with focus-visible indicators, when ensuring form fields have proper labels connected via htmlFor and IDs, when associating error messages with form inputs using aria-describedby and aria-invalid, when adding screen reader support with sr-only classes for hidden context text, when implementing role attributes for status messages (role="status", role="alert"), when checking color contrast ratios for WCAG AA compliance (4.5:1 for text), when implementing focus indicators using focus-visible:ring Tailwind classes, when creating icon-only buttons that need aria-label attributes, when implementing loading states that should be announced to screen readers with role="status" and aria-live="polite", when working with modals/dialogs that need focus trapping and return focus on close, when ensuring semantic HTML elements are used (<header>, <nav>, <main>, <button>, not <div onClick>), when testing with screen readers (NVDA, JAWS, VoiceOver) or keyboard-only navigation, or when implementing toggle buttons with aria-expanded state. This skill ensures the Kagent UI is usable by everyone, including users with disabilities, by leveraging Radix UI's built-in accessibility features and following WAI-ARIA best practices.
---

## When to use this skill

- When creating new React components in `ui/src/components/`
- When working with Radix UI primitives (Dialog, Dropdown, Select, Tooltip, etc.)
- When adding ARIA attributes to elements (`aria-label`, `aria-describedby`, `aria-invalid`)
- When implementing form accessibility with labels, descriptions, and error messages
- When ensuring keyboard navigation works for all interactive elements
- When adding focus indicators with `focus-visible:ring` classes
- When implementing screen reader support with `sr-only` or `role` attributes
- When checking color contrast meets WCAG AA standards (4.5:1 for text)
- When creating buttons, links, or other interactive elements
- When working with modals/dialogs that need focus trapping
- When implementing loading states that should be announced to screen readers
- When ensuring semantic HTML is used (`<header>`, `<nav>`, `<main>`, `<button>`)

# Frontend Accessibility

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle frontend accessibility.

## Instructions

For details, refer to the information provided in this file:
[frontend accessibility](../../../agent-os/standards/frontend/accessibility.md)
