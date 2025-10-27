## Responsive design best practices

### Mobile-First Approach

From `ui/src/components/Header.tsx` (lines 31-232):

**Start with mobile layout, then add larger breakpoints:**

```tsx
// Base (mobile) - no prefix
<div className="py-4 px-4">

// Tablet and up - md: prefix
<div className="py-4 md:py-8">

// Desktop and up - lg: prefix
<div className="space-x-2 lg:space-x-4">
```

### Tailwind Breakpoints

From `ui/tailwind.config.ts`:

**Standard breakpoints used throughout the project:**

| Breakpoint | Min Width | Device Target |
|------------|-----------|---------------|
| (default)  | 0px       | Mobile phones |
| `sm:`      | 640px     | Large phones  |
| `md:`      | 768px     | Tablets       |
| `lg:`      | 1024px    | Laptops       |
| `xl:`      | 1280px    | Desktops      |
| `2xl:`     | 1536px    | Large screens |

**Usage:**

```tsx
<div className="
  w-full        // 100% width on mobile
  md:w-1/2      // 50% width on tablet
  lg:w-1/3      // 33% width on desktop
">
```

### Navigation Patterns

From `ui/src/components/Header.tsx`:

**Desktop Navigation (lines 48-120):**

```tsx
<div className="hidden md:flex items-center space-x-2 lg:space-x-4">
  <Link href="/agents">Agents</Link>
  <Link href="/models">Models</Link>
  <Link href="/servers">Servers</Link>
  <Link href="/tools">Tools</Link>
</div>
```

**Mobile Navigation (lines 39-45, 138-215):**

```tsx
{/* Mobile menu button */}
<button
  className="md:hidden p-2"
  onClick={toggleMenu}
  aria-label="Toggle menu"
>
  {isMenuOpen ? <X /> : <Menu />}
</button>

{/* Mobile menu dropdown */}
{isMenuOpen && (
  <div className="md:hidden pt-4 pb-2">
    <Link href="/agents">Agents</Link>
    <Link href="/models">Models</Link>
    {/* ... */}
  </div>
)}
```

### Responsive Spacing

**Padding and margins adapt to screen size:**

```tsx
// Header spacing
<header className="py-4 md:py-8 px-4 md:px-6">

// Container spacing
<div className="space-x-2 lg:space-x-4">

// Grid gaps
<div className="grid gap-4 md:gap-6 lg:gap-8">
```

### Grid Layouts

From `ui/src/components/AgentGrid.tsx`:

```tsx
<div className="
  grid
  grid-cols-1          // 1 column on mobile
  md:grid-cols-2       // 2 columns on tablet
  lg:grid-cols-3       // 3 columns on desktop
  gap-4
">
  {agents.map(agent => <AgentCard key={agent.id} agent={agent} />)}
</div>
```

### Chat Interface Responsive

From `ui/src/components/chat/ChatInterface.tsx` (line 396):

**Input area adjusts position and styling:**

```tsx
<div className="
  w-full
  sticky
  bg-secondary
  bottom-0              // Flush to bottom on mobile
  md:bottom-2           // Slight gap on desktop
  rounded-none          // No rounding on mobile
  md:rounded-lg         // Rounded on desktop
  p-4
  border
">
  <Textarea placeholder="Type your message..." />
</div>
```

### Dialog/Modal Responsive

From `ui/src/components/ConfirmDialog.tsx` (lines 24-44):

**Dialogs adapt size and button layout:**

```tsx
<DialogContent className="sm:max-w-md">
  <DialogHeader className="
    flex flex-col
    items-center         // Center on mobile
    sm:items-start       // Left-align on desktop
  ">
    <DialogTitle>Confirm Action</DialogTitle>
  </DialogHeader>

  <DialogFooter className="flex gap-2">
    <Button className="
      flex-1              // Full width on mobile
      sm:flex-initial     // Auto width on desktop
    ">
      Cancel
    </Button>
    <Button className="flex-1 sm:flex-initial">
      Confirm
    </Button>
  </DialogFooter>
</DialogContent>
```

### useIsMobile Hook

From `ui/src/hooks/use-mobile.tsx` (lines 1-19):

**JavaScript-based responsive logic:**

```tsx
const MOBILE_BREAKPOINT = 768  // Matches md: breakpoint

export function useIsMobile() {
  const [isMobile, setIsMobile] = useState<boolean | undefined>(undefined)

  useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`)
    const onChange = () => {
      setIsMobile(window.innerWidth < MOBILE_BREAKPOINT)
    }
    mql.addEventListener("change", onChange)
    setIsMobile(window.innerWidth < MOBILE_BREAKPOINT)
    return () => mql.removeEventListener("change", onChange)
  }, [])

  return !!isMobile
}

// Usage in components
const isMobile = useIsMobile()

{isMobile ? <MobileView /> : <DesktopView />}
```

### Typography Responsive

**Font sizes scale with screen size:**

```tsx
<h1 className="
  text-2xl             // Mobile
  md:text-3xl          // Tablet
  lg:text-4xl          // Desktop
  font-bold
">
  Heading
</h1>

<p className="text-sm md:text-base">
  Body text scales from 14px to 16px
</p>
```

### Touch-Friendly Design

**Tap targets sized appropriately:**

```tsx
// Minimum 44x44px touch targets
<button className="
  h-11                 // 44px height
  w-11                 // 44px width (for icon buttons)
  px-4 py-2            // Adequate padding for text buttons
">
  <Icon className="w-6 h-6" />  // Large enough icons
</button>
```

### Container Max Width

```tsx
<div className="
  container            // Centers and constrains width
  mx-auto              // Auto horizontal margins
  px-4                 // Mobile padding
  md:px-6              // Desktop padding
  max-w-7xl            // Maximum content width
">
  {/* Content */}
</div>
```

### Image Responsive

```tsx
<img
  src="/image.jpg"
  alt="Description"
  className="
    w-full              // Full width of container
    h-auto              // Maintain aspect ratio
    object-cover        // Cover container (for fixed heights)
    max-w-full          // Never exceed container
  "
/>
```

### Hide/Show Content

**Show/hide elements at different breakpoints:**

```tsx
// Show only on mobile
<div className="block md:hidden">
  Mobile content
</div>

// Hide on mobile, show on tablet+
<div className="hidden md:block">
  Desktop content
</div>

// Show on mobile and large desktop, hide on tablet
<div className="block md:hidden lg:block">
  Mobile and large desktop
</div>
```

### Flex Direction

**Stack vertically on mobile, horizontally on desktop:**

```tsx
<div className="
  flex
  flex-col            // Vertical stack on mobile
  md:flex-row         // Horizontal on desktop
  gap-4
">
  <div>Item 1</div>
  <div>Item 2</div>
</div>
```

### Testing Requirements

From `ui/src/components/`:

**Test across device sizes:**
1. **Mobile**: 375px (iPhone SE), 390px (iPhone 12)
2. **Tablet**: 768px (iPad), 1024px (iPad Pro)
3. **Desktop**: 1280px, 1440px, 1920px

**Testing approach:**
- Use browser DevTools responsive mode
- Test actual devices when possible
- Verify touch interactions on mobile
- Check horizontal scrolling (should be none)
- Ensure readable font sizes without zoom
- Verify nav accessibility on all sizes

### Performance on Mobile

**Optimize for mobile networks:**

```tsx
// Lazy load images
<img loading="lazy" src="..." alt="..." />

// Responsive images (Next.js Image component)
import Image from 'next/image'

<Image
  src="/hero.jpg"
  width={800}
  height={600}
  alt="Hero"
  sizes="(max-width: 768px) 100vw, 50vw"
/>
```

### Sidebar Responsive

From `ui/src/components/sidebars/SessionsSidebar.tsx`:

**Sidebars hidden on mobile, shown on desktop:**

```tsx
<aside className="
  hidden              // Hidden on mobile
  lg:block            // Show on large screens
  w-64                // Fixed width on desktop
  border-r
">
  {/* Sidebar content */}
</aside>

// Mobile: Use Sheet/Drawer component
<Sheet>
  <SheetTrigger className="lg:hidden">
    <Menu />  {/* Only show trigger on mobile */}
  </SheetTrigger>
  <SheetContent side="left">
    {/* Same sidebar content */}
  </SheetContent>
</Sheet>
```

### Form Layout Responsive

```tsx
<form className="space-y-4">
  {/* Single column on mobile, two columns on desktop */}
  <div className="
    grid
    grid-cols-1        // 1 column mobile
    md:grid-cols-2     // 2 columns desktop
    gap-4
  ">
    <FormField name="firstName" />
    <FormField name="lastName" />
  </div>

  {/* Full width field */}
  <FormField name="email" />

  {/* Buttons stack on mobile, inline on desktop */}
  <div className="
    flex
    flex-col
    md:flex-row
    gap-2
  ">
    <Button type="submit" className="flex-1 md:flex-initial">
      Submit
    </Button>
    <Button variant="outline" className="flex-1 md:flex-initial">
      Cancel
    </Button>
  </div>
</form>
```

### Best Practices

1. **Mobile First**: Always design mobile layout first
2. **Breakpoint Consistency**: Use standard Tailwind breakpoints (`md:`, `lg:`)
3. **Touch Targets**: Minimum 44x44px for all interactive elements
4. **Readable Text**: Minimum 16px font size on mobile (no zoom required)
5. **Test Real Devices**: Don't rely solely on browser DevTools
6. **No Horizontal Scroll**: Content should fit viewport at all sizes
7. **Performance**: Optimize images and assets for mobile networks
8. **Navigation**: Hamburger menu on mobile, full nav on desktop
9. **Spacing**: Reduce padding/margins on mobile to maximize content area
10. **Forms**: Single column on mobile, multi-column on desktop
