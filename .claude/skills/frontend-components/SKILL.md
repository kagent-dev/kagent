---
name: Frontend Components
description: Build React components using TypeScript, Next.js App Router, React Context, and Radix UI primitives for the Kagent web UI. Use this skill when creating new React components in ui/src/components/, when organizing components by feature domain (chat/, sidebars/, create/, models/, onboarding/, tools/), when implementing the container/presentational component pattern (container manages state and effects, presentational receives props), when using React hooks like useState for local UI state, useEffect for data fetching and side effects, useContext for accessing global state from Context providers, or useRef for non-UI state that doesn't trigger re-renders (like AbortController or isFirstChunk flags), when working with Radix UI wrapper components in ui/src/components/ui/ (Button, Input, Dialog, Dropdown, Select, Tooltip, Tabs), when implementing component composition patterns with Context providers and children props, when defining TypeScript interfaces for component props with optional props and default values, when managing local component state (isOpen, currentInput, errors) versus global Context state (agents, sessions from AgentsProvider), when implementing event handlers following the handle{Action} naming convention (handleSubmit, handleClick, handleDelete), when working with forms using React Hook Form's Controller and FormField components, when building reusable UI components with the asChild Radix composition pattern and forwardRef for ref forwarding, when implementing data fetching in useEffect with async functions and proper error handling, when creating custom hooks (useAgents, useFormField) for sharing logic, when using the cn() utility to merge Tailwind classes conditionally, or when organizing component files with hooks first, then event handlers, then render logic. This skill covers component architecture, React hooks, state management patterns (local vs Context), props interfaces, TypeScript integration, and Radix UI composition for the Next.js frontend.
---

## When to use this skill

- When creating new React components in `ui/src/components/`
- When organizing components by feature (`chat/`, `sidebars/`, `create/`, `models/`, `onboarding/`)
- When implementing container vs presentational component patterns
- When using React hooks (`useState`, `useEffect`, `useContext`, `useRef`)
- When working with Radix UI wrappers in `ui/src/components/ui/`
- When implementing component composition with children props
- When defining TypeScript interfaces for component props
- When managing local state with `useState` vs global state with Context API
- When implementing event handlers (`handleClick`, `handleSubmit`, `onChange`)
- When working with forms using React Hook Form
- When creating reusable components with configurable props and defaults
- When implementing custom hooks (`useAgents`, `useIsMobile`)
- When using refs for DOM manipulation or non-UI state
- When fetching data with Next.js Server Actions in useEffect

# Frontend Components

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle frontend components.

## Instructions

For details, refer to the information provided in this file:
[frontend components](../../../agent-os/standards/frontend/components.md)
