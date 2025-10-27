## CSS best practices

### Tailwind CSS Framework

From `ui/package.json` (line 76) and `ui/tailwind.config.ts`:

**Stack:**
- Tailwind CSS 3.4.17 - Utility-first CSS framework
- `@tailwindcss/typography` - Prose styling for markdown
- `tailwindcss-animate` - Pre-built animations
- `class-variance-authority` (CVA) - Type-safe variant management
- `tailwind-merge` (twMerge) - Intelligent class merging
- `clsx` - Conditional class names

### Utility-First Approach

**Use Tailwind utilities directly in JSX:**

```tsx
<button className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors">
  Click me
</button>
```

**Avoid custom CSS** - prefer Tailwind utilities for:
- Spacing: `p-4`, `m-2`, `gap-4`, `space-x-2`
- Layout: `flex`, `grid`, `items-center`, `justify-between`
- Colors: `bg-primary`, `text-foreground`, `border-border`
- Typography: `text-sm`, `font-medium`, `leading-none`
- Effects: `shadow-sm`, `rounded-lg`, `opacity-50`

### Class Variance Authority (CVA)

From `ui/src/components/ui/button.tsx` (lines 7-35):

**Define component variants with CVA:**

```tsx
import { cva, type VariantProps } from "class-variance-authority"

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 rounded-md font-medium transition-colors",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        outline: "border border-input hover:bg-accent",
        ghost: "hover:bg-accent hover:text-accent-foreground",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 px-3 text-xs",
        lg: "h-10 px-8",
        icon: "h-9 w-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

// Usage
<Button variant="destructive" size="lg">Delete</Button>
```

### Class Merging Utility

From `ui/src/lib/utils.ts`:

```tsx
import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Usage - intelligently merges classes
<div className={cn(
  "px-4 py-2",
  isActive && "bg-primary text-white",
  className  // Allow override from props
)} />
```

### Design Tokens (Theme)

From `ui/tailwind.config.ts` (lines 12-62):

**HSL-based color system:**

```js
colors: {
  background: "hsl(var(--background))",
  foreground: "hsl(var(--foreground))",
  primary: {
    DEFAULT: "hsl(var(--primary))",
    foreground: "hsl(var(--primary-foreground))",
  },
  secondary: {
    DEFAULT: "hsl(var(--secondary))",
    foreground: "hsl(var(--secondary-foreground))",
  },
  muted: {
    DEFAULT: "hsl(var(--muted))",
    foreground: "hsl(var(--muted-foreground))",
  },
  accent: {
    DEFAULT: "hsl(var(--accent))",
    foreground: "hsl(var(--accent-foreground))",
  },
  destructive: {
    DEFAULT: "hsl(var(--destructive))",
    foreground: "hsl(var(--destructive-foreground))",
  },
  card: {
    DEFAULT: "hsl(var(--card))",
    foreground: "hsl(var(--card-foreground))",
  },
  border: "hsl(var(--border))",
  input: "hsl(var(--input))",
  ring: "hsl(var(--ring))",
}
```

**CSS Variables** (defined in `ui/src/app/globals.css`):

```css
@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 222.2 84% 4.9%;
    --primary: 222.2 47.4% 11.2%;
    /* ... */
  }

  .dark {
    --background: 222.2 84% 4.9%;
    --foreground: 210 40% 98%;
    /* ... */
  }
}
```

### Dark Mode Support

From `ui/tailwind.config.ts` (line 4):

```js
darkMode: ["class"]  // Use class-based dark mode
```

**Usage:**

```tsx
// Automatically switches based on .dark class on <html>
<div className="bg-background text-foreground">
  Content adapts to light/dark mode
</div>

// Explicit dark mode styling
<div className="bg-white dark:bg-black">
  Light background, dark background in dark mode
</div>
```

### Responsive Design

**Mobile-first breakpoints:**

```tsx
<div className="w-full md:w-1/2 lg:w-1/3">
  // Full width mobile, half on tablet, third on desktop
</div>

<nav className="hidden md:flex">
  // Hidden on mobile, flex on desktop
</nav>

<button className="md:hidden">
  // Visible on mobile, hidden on desktop
</button>
```

**Standard breakpoints:**
- `sm:` 640px
- `md:` 768px
- `lg:` 1024px
- `xl:` 1280px
- `2xl:` 1536px

### Typography

From `ui/tailwind.config.ts` and `ui/src/app/layout.tsx` (line 12):

**Font family:**
- Geist Sans (default)
- Geist Mono (code)

**Typography plugin** for prose content:

```tsx
<div className="prose dark:prose-invert max-w-none">
  {/* Markdown content styled automatically */}
</div>
```

### Animations

From `ui/tailwind.config.ts` (line 67-81):

```js
animation: {
  "accordion-down": "accordion-down 0.2s ease-out",
  "accordion-up": "accordion-up 0.2s ease-out",
}
keyframes: {
  "accordion-down": {
    from: { height: "0" },
    to: { height: "var(--radix-accordion-content-height)" },
  },
}
```

**Usage:**

```tsx
<div className="animate-accordion-down">Content</div>
```

**Transition utilities:**

```tsx
<button className="transition-colors hover:bg-primary/90">
  Smooth color transition
</button>

<div className="transition-all duration-300 ease-in-out">
  Smooth all property transitions
</div>
```

### Component-Specific Styling

**Conditional classes from props:**

From `ui/src/components/ConfirmDialog.tsx` (lines 40-42):

```tsx
<Button
  className={cn(
    "flex-1 sm:flex-initial",
    variant === "destructive" && "bg-red-600 hover:bg-red-700 text-white"
  )}
>
  Confirm
</Button>
```

**Error states:**

From `ui/src/components/models/new/BasicInfoSection.tsx` (line 64):

```tsx
<Input
  className={errors.name ? "border-destructive" : ""}
  placeholder="Enter name..."
/>
{errors.name && (
  <p className="text-destructive text-sm mt-1">{errors.name}</p>
)}
```

### Best Practices

1. **Use semantic colors** - `bg-primary` not `bg-blue-500`
2. **Leverage HSL variables** - Enables theme switching without rebuild
3. **Avoid inline styles** - Use Tailwind utilities
4. **Use CVA for variants** - Type-safe component variants
5. **Use cn() utility** - Smart class merging with twMerge
6. **Mobile-first** - Start with mobile, add larger breakpoints
7. **Consistent spacing** - Use Tailwind's spacing scale (4px increments)
8. **Focus states** - Always include `focus-visible:ring` for accessibility

### Performance

**Purging unused CSS:**

Tailwind automatically removes unused utilities in production builds via `next build`.

**Custom purge config** (if needed):

```js
// tailwind.config.ts
content: [
  "./src/**/*.{js,ts,jsx,tsx}",
]
```
