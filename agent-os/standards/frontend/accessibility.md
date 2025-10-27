## UI accessibility best practices

### Radix UI Foundation

From `ui/package.json` (lines 18-33):

**All UI primitives use Radix UI** - WAI-ARIA compliant out of the box:
- `@radix-ui/react-dialog` - Accessible modals with focus trapping
- `@radix-ui/react-dropdown-menu` - Keyboard-navigable menus
- `@radix-ui/react-select` - Accessible select inputs
- `@radix-ui/react-checkbox` - Accessible checkboxes
- `@radix-ui/react-tooltip` - Screen reader friendly tooltips
- `@radix-ui/react-tabs` - ARIA tabs pattern
- `@radix-ui/react-accordion` - Collapsible sections

**Benefits:**
- Automatic ARIA attributes
- Keyboard navigation built-in
- Focus management
- Screen reader announcements

### Semantic HTML

From `ui/src/components/Header.tsx`:

**Use appropriate HTML elements:**

```tsx
<header className="...">
  <nav className="...">
    <Link href="/agents">Agents</Link>
    <Link href="/models">Models</Link>
  </nav>

  <button aria-label="Toggle menu" onClick={toggleMenu}>
    <Menu className="h-6 w-6" />
  </button>
</header>
```

**Semantic elements used:**
- `<header>` - Page/section headers
- `<nav>` - Navigation sections
- `<main>` - Main content area
- `<button>` - Interactive actions (not `<div onClick>`)
- `<a>` / `<Link>` - Navigation
- `<form>` - Form submissions

### Form Accessibility

From `ui/src/components/ui/form.tsx` (lines 1-178):

**1. Labels Connected to Inputs**

```tsx
<FormLabel htmlFor={formItemId}>
  Email Address
</FormLabel>
<FormControl>
  <Input
    id={formItemId}  // Automatic ID generation
    {...field}
  />
</FormControl>
```

**2. Error Messaging**

```tsx
<FormControl>
  <Input
    aria-describedby={
      !error
        ? `${formDescriptionId}`
        : `${formDescriptionId} ${formMessageId}`
    }
    aria-invalid={!!error}
  />
</FormControl>
{error && <FormMessage id={formMessageId}>{error.message}</FormMessage>}
```

**3. Field Descriptions**

```tsx
<FormDescription id={formDescriptionId}>
  Enter your email address to receive updates
</FormDescription>
```

### ARIA Attributes

From `ui/src/components/chat/ChatMessage.tsx` (lines 132-146):

**Button Labels:**

```tsx
<button
  onClick={() => handleFeedback(true)}
  className="..."
  aria-label="Thumbs up"
>
  <ThumbsUp className="w-4 h-4" />
</button>

<button
  onClick={() => handleFeedback(false)}
  aria-label="Thumbs down"
>
  <ThumbsDown className="w-4 h-4" />
</button>
```

**Toggle Buttons:**

From `ui/src/components/Header.tsx` (lines 39-45):

```tsx
<button
  onClick={toggleMenu}
  aria-label="Toggle menu"
  aria-expanded={isMenuOpen}
>
  {isMenuOpen ? <X /> : <Menu />}
</button>
```

### Keyboard Navigation

**Focus Indicators:**

From `ui/src/components/ui/button.tsx` (line 8):

```tsx
const buttonVariants = cva(
  "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
  // ...
)
```

**All interactive elements include:**
- `focus-visible:ring` - Visible focus ring for keyboard users
- `:focus-visible` (not `:focus`) - Only shows for keyboard, not mouse clicks
- `disabled:pointer-events-none` - Prevents interaction when disabled
- `disabled:opacity-50` - Visual indication of disabled state

**Tab Order:**
- Use semantic HTML for natural tab order
- Avoid `tabindex` values other than -1, 0
- Modals trap focus automatically (via Radix Dialog)

### Screen Reader Support

**Hidden Text for Context:**

```tsx
<button aria-label="Delete agent my-agent">
  <Trash className="w-4 h-4" />
  <span className="sr-only">Delete agent my-agent</span>
</button>
```

**Status Messages:**

```tsx
<div role="status" aria-live="polite">
  {isLoading && <Spinner />}
  {isLoading && <span className="sr-only">Loading...</span>}
</div>
```

**Landmark Regions:**

Radix UI components automatically provide appropriate roles:
- Dialog: `role="dialog"` with `aria-labelledby`
- Alert: `role="alert"` with `aria-describedby`
- Menu: `role="menu"` with `aria-orientation`

### Color Contrast

From `ui/tailwind.config.ts`:

**Semantic color system** ensures sufficient contrast:
- `text-foreground` on `bg-background` - WCAG AA compliant
- `text-primary-foreground` on `bg-primary` - High contrast
- `text-destructive-foreground` on `bg-destructive` - Accessible red

**Don't rely solely on color:**

```tsx
// ❌ BAD - Color only
<span className="text-red-500">Error</span>

// ✅ GOOD - Icon + text + semantic HTML
<div role="alert" className="text-destructive">
  <AlertCircle className="inline w-4 h-4" />
  <span>Error: Invalid input</span>
</div>
```

### Loading States

From `ui/src/components/chat/ChatInterface.tsx`:

**Loading indicators with text alternatives:**

```tsx
{isLoading && (
  <div role="status" className="flex items-center gap-2">
    <Spinner />
    <span className="sr-only">Loading messages...</span>
  </div>
)}
```

### Form Validation

From `ui/src/components/models/new/BasicInfoSection.tsx` (lines 64-86):

**Visual + Programmatic errors:**

```tsx
<Input
  value={name}
  onChange={(e) => onNameChange(e.target.value)}
  className={errors.name ? "border-destructive" : ""}
  aria-invalid={!!errors.name}
  aria-describedby={errors.name ? "name-error" : undefined}
/>
{errors.name && (
  <p id="name-error" className="text-destructive text-sm" role="alert">
    {errors.name}
  </p>
)}
```

### Image Alternatives

**All images have alt text:**

```tsx
// Decorative images
<img src="/logo.svg" alt="" role="presentation" />

// Meaningful images
<img
  src="/provider-logo.png"
  alt="OpenAI logo"
  className="w-6 h-6"
/>
```

**Icon-only buttons need labels:**

```tsx
<button aria-label="Close dialog">
  <X className="w-4 h-4" />
</button>
```

### Modal Accessibility

Radix Dialog provides:
- Focus trap - keeps focus inside modal
- Escape key - closes modal
- Click outside - closes modal (optional)
- Return focus - returns to trigger element on close
- `aria-labelledby` - connects title to dialog
- `aria-describedby` - connects description to dialog

From `ui/src/components/ui/dialog.tsx`:

```tsx
<Dialog>
  <DialogTrigger asChild>
    <Button>Open</Button>
  </DialogTrigger>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>Dialog Title</DialogTitle>
      <DialogDescription>Dialog description</DialogDescription>
    </DialogHeader>
    {/* Content */}
  </DialogContent>
</Dialog>
```

### Testing Recommendations

1. **Keyboard Navigation**: Tab through entire interface
2. **Screen Reader**: Test with NVDA/JAWS (Windows) or VoiceOver (Mac)
3. **Color Blindness**: Use browser extensions to simulate
4. **Zoom**: Test at 200% zoom level
5. **No Mouse**: Navigate entire app without mouse

### Accessibility Checklist

- ✅ All images have alt text
- ✅ All buttons have accessible names (text or aria-label)
- ✅ Forms have proper labels and error messages
- ✅ Color contrast meets WCAG AA (4.5:1 for text)
- ✅ Focus indicators are visible
- ✅ Keyboard navigation works for all interactions
- ✅ Screen readers can navigate and understand content
- ✅ Loading states announced to screen readers
- ✅ Modals trap focus and return focus on close
- ✅ Error messages associated with form fields
