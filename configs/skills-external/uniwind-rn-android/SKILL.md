---
name: uniwind-rn-android
description: "Design or review Expo and React Native Android UI with Uniwind Pro, Material patterns, edge-to-edge layout, and touch ergonomics."
---

# Android Native Feel — Expo + React Native + Uniwind Pro

Make React Native screens feel credible on Android by respecting Material 3, edge-to-edge layouts, 48dp touch targets, tonal surfaces, and Android interaction patterns.

Do not make Android look like iOS with different fonts. Android deserves its own visual and interaction logic.

## Core Android Qualities

Great Android interfaces tend to feel:

- structured
- adaptive
- clear in hierarchy
- comfortable at 48dp touch scale
- explicit in state changes
- tonal rather than heavily shadowed
- responsive to system bars
- comfortable with FABs and bottom navigation
- aligned with Material 3 color, shape, and type roles

## Android Rules

### 1. Minimum touch target: 48×48 dp

Android controls should generally have a 48×48 touch target.

The visible icon can be 20–24, but the pressable area should be 48.

Good:

```tsx
<Pressable
  accessibilityRole="button"
  accessibilityLabel="Search"
  className="
    size-12 items-center justify-center rounded-full
    active:bg-primary/10 transition-colors duration-100
  "
>
  <SearchIcon size={24} />
</Pressable>
```

If using a smaller visible control, use `hitSlop`, but ensure touch regions do not overlap.

### 2. Use Android edge-to-edge deliberately

Android apps increasingly run edge-to-edge. Treat system bars as part of the composition.

Use:

- `pt-safe` for top app bars
    
- `pb-safe` for bottom nav or bottom actions
    
- `bottom-safe-offset-*` for FABs
    
- scrims or opaque surfaces when content behind system bars hurts readability
    

Top app bar:

```tsx
<View className="bg-background pt-safe">
  <View className="min-h-16 flex-row items-center gap-3 px-4">
    <Pressable className="size-12 items-center justify-center rounded-full active:bg-muted">
      <ArrowLeftIcon size={24} />
    </Pressable>

    <Text className="flex-1 text-xl font-sans-medium text-foreground">
      Details
    </Text>
  </View>
</View>
```

Bottom bar:

```tsx
<View className="absolute inset-x-0 bottom-0 border-t border-border bg-background px-2 pb-safe pt-2">
  <View className="min-h-16 flex-row items-center justify-around">
    {/* nav items */}
  </View>
</View>
```

### 3. Use Material-like tonal surfaces

Android Material 3 often feels better with tonal surface separation than with heavy iOS-style shadows.

Prefer:

- `bg-background`
    
- `bg-surface`
    
- `bg-surface-container`
    
- `bg-primary-container`
    
- `text-on-primary-container`
    
- `border-border`
    

If the theme does not have these tokens, recommend adding them.

Suggested Android-oriented tokens:

```css
@layer theme {
  :root {
    @variant light {
      --color-background: #fffbff;
      --color-foreground: #1d1b20;
      --color-surface: #fffbff;
      --color-surface-container-low: #f7f2fa;
      --color-surface-container: #f3edf7;
      --color-surface-container-high: #ece6f0;
      --color-primary: #6750a4;
      --color-on-primary: #ffffff;
      --color-primary-container: #eaddff;
      --color-on-primary-container: #21005d;
      --color-border: #cac4d0;
    }
  }
}
```

Use the app’s actual brand colors, but keep the role structure.

### 4. Android top app bars should react to scroll

For long screens:

- top app bar can start visually flat
    
- add separation after scroll
    
- avoid permanent heavy shadows
    
- use border or tonal fill instead of iOS blur
    

Before scroll:

```tsx
<View className="bg-background pt-safe">
  <View className="min-h-16 flex-row items-center px-4">
    <Text className="text-xl font-sans-medium text-foreground">Inbox</Text>
  </View>
</View>
```

After scroll:

```tsx
<View className="bg-surface-container border-b border-border pt-safe">
  <View className="min-h-16 flex-row items-center px-4">
    <Text className="text-xl font-sans-medium text-foreground">Inbox</Text>
  </View>
</View>
```

### 5. Use FABs when they clarify the primary action

FABs are more Android-native than iOS-native.

Use a FAB when:

- there is one dominant creation action
    
- it applies to the whole screen
    
- it is useful while scrolling
    
- it does not compete with bottom navigation
    

Pattern:

```tsx
<Pressable
  accessibilityRole="button"
  accessibilityLabel="Create"
  className="
    absolute right-4 bottom-safe-offset-4
    h-14 min-w-14 rounded-2xl bg-primary px-4
    flex-row items-center justify-center gap-2
    active:opacity-90 active:scale-[0.98]
    transition-transform transition-opacity duration-100
  "
>
  <PlusIcon size={24} color="white" />
  <Text className="font-sans-medium text-on-primary">Create</Text>
</Pressable>
```

Avoid FABs for:

- destructive actions
    
- secondary actions
    
- screens with already-visible primary CTAs
    
- actions that only affect one row
    

### 6. Android press states should be visible

Android expects stronger state feedback than iOS.

Good state feedback:

- subtle state layer color
    
- opacity change
    
- small scale for custom prominent buttons
    
- ripple-like background change
    

Examples:

```tsx
<Pressable
  className="
    min-h-12 rounded-full bg-primary px-6
    items-center justify-center
    active:bg-primary/90 active:scale-[0.98]
    transition-colors transition-transform duration-100
  "
>
  <Text className="text-base font-sans-medium text-on-primary">
    Continue
  </Text>
</Pressable>
```

Text button:

```tsx
<Pressable
  className="
    min-h-12 rounded-full px-4
    items-center justify-center
    active:bg-primary/10 transition-colors duration-100
  "
>
  <Text className="text-sm font-sans-medium text-primary">
    Cancel
  </Text>
</Pressable>
```

### 7. Android typography should map to Material roles

Use Android-friendly type roles:

|Role|Size / line|Use|
|---|---|---|
|display|36+|Rare hero moments|
|headline|24–32|Screen introductions|
|title large|22/28|App bar or screen title|
|title medium|16/24 medium|Cards, dialogs|
|body large|16/24|Main body|
|body medium|14/20|Secondary body|
|label large|14/20 medium|Buttons|
|label small|11–12/16|Metadata|

Pattern:

```tsx
<Text className="text-[22px] leading-7 font-sans-medium text-foreground">
  Payment methods
</Text>

<Text className="mt-1 text-base leading-6 text-muted">
  Choose how you want to pay.
</Text>
```

### 8. Android forms need clear fields and enough vertical rhythm

Text inputs should be at least 48 high. Use visible focus states.

```tsx
<TextInput
  className="
    min-h-14 rounded-xl border border-border bg-surface px-4
    text-base text-foreground
    focus:border-primary focus:bg-surface-container-low
  "
  placeholder="Email"
  placeholderTextColorClassName="accent-muted"
  cursorColorClassName="accent-primary"
  selectionColorClassName="accent-primary"
  underlineColorAndroidClassName="accent-transparent"
/>
```

Do not let Android show the default underline unless it is intentionally part of the design. Set `underlineColorAndroidClassName="accent-transparent"` for custom fields.

### 9. Bottom navigation should be stable and thumb-friendly

Use bottom navigation for top-level destinations.

Rules:

- 3–5 destinations
    
- never put arbitrary actions in bottom nav
    
- active state must be obvious
    
- icons need labels unless the icons are universally clear
    
- respect `pb-safe`
    
- keep item targets at least 48 high
    

Pattern:

```tsx
<View className="absolute inset-x-0 bottom-0 border-t border-border bg-surface px-2 pb-safe pt-2">
  <View className="min-h-16 flex-row items-center justify-around">
    {tabs.map((tab) => (
      <Pressable
        key={tab.key}
        data-selected={tab.key === active}
        className="
          min-h-12 min-w-16 items-center justify-center rounded-2xl px-3
          active:bg-primary/10
          data-[selected=true]:bg-primary-container
          transition-colors duration-100
        "
      >
        <tab.Icon />
        <Text
          className="
            mt-1 text-xs font-sans-medium text-muted
            data-[selected=true]:text-on-primary-container
          "
        >
          {tab.label}
        </Text>
      </Pressable>
    ))}
  </View>
</View>
```

### 10. Android motion can be slightly more explicit than iOS

Use motion to reinforce hierarchy and state.

Good Android motion:

- container transform for dialogs/sheets
    
- fade-through for content swaps
    
- subtle layout transitions for list changes
    
- clear state-layer transitions
    
- short, responsive timing
    

Use Uniwind Pro layout transitions for list mutation:

```tsx
{items.map((item) => (
  <View
    key={item.id}
    className="
      rounded-xl bg-surface-container p-4
      uw-entering-fade-in-up uw-exiting-fade-out
      uw-layout-linear-transition
    "
  >
    <Text className="text-foreground">{item.title}</Text>
  </View>
))}
```

Avoid novelty presets like `glitch`, `tada`, `rubber-band`, and `jello` in normal product UI.

### 11. Android dark mode needs tonal contrast, not just inversion

Do not simply invert white and black. Android dark themes should preserve contrast through surface roles.

Use different dark tokens for:

- background
    
- surface
    
- surface container
    
- primary
    
- primary container
    
- borders
    
- muted text
    

Good dark-mode pattern:

```tsx
<View className="bg-background">
  <View className="bg-surface-container border border-border">
    <Text className="text-foreground">Title</Text>
    <Text className="text-muted">Subtitle</Text>
  </View>
</View>
```

### 12. Use Android platform selectors for Android-specific affordances

```tsx
<Pressable
  className="
    ios:rounded-xl ios:active:opacity-80
    android:rounded-full android:active:bg-primary/10
    min-h-12 px-4 items-center justify-center
  "
>
  <Text className="android:text-sm ios:text-base font-sans-medium">
    Action
  </Text>
</Pressable>
```

Do not make a single visual treatment pretend to be native on both platforms.

## Android Anti-patterns

Avoid:

- iOS-style grouped settings copied directly to Android
    
- permanent heavy drop shadows everywhere
    
- tiny 36-high controls
    
- bottom bars without `pb-safe`
    
- floating buttons that cover list content
    
- silent press states
    
- default Android TextInput underline mixed with custom fields
    
- iOS blur-heavy navigation
    
- making Android cards too round and glossy
    
- top bars that ignore edge-to-edge system bars
    
- destructive actions in FABs
    
- bottom nav with more than five destinations
    

## Android Review Output Format

Use markdown tables grouped by principle.

### Example

#### Android touch targets

|Before|After|Reason|
|---|---|---|
|Icon button used `size-9`|Changed to `size-12`|Meets Android 48dp touch target|
|Adjacent small controls used `hitSlop={16}`|Reduced hitSlop and increased visual spacing|Prevents overlapping touch areas|

#### Android edge-to-edge

|Before|After|Reason|
|---|---|---|
|Bottom nav used `pb-3`|Replaced with `pb-safe`|Protects nav from system bar overlap|
|FAB used `bottom-6`|Replaced with `bottom-safe-offset-4`|Keeps FAB above gesture/nav area|

#### Android Material feel

| Before                                | After                                    | Reason                                       |
| ------------------------------------- | ---------------------------------------- | -------------------------------------------- |
| Cards used iOS blur and shadow        | Switched to tonal `bg-surface-container` | Feels more Material and less iOS             |
| Text button had only opacity feedback | Added `active:bg-primary/10`             | Android expects visible state layer feedback |
