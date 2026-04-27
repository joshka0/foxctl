---
name: uniwind-excellence-rn
description: "Build polished Expo and React Native UI with Uniwind Pro, covering screens, components, navigation, theming, and native feel."
---

# Mobile Interface Excellence — Expo + React Native + Uniwind Pro

Design mobile interfaces that feel native, calm, fast, and physically credible on iOS and Android.

This skill sits on top of the Uniwind skill. Use the Uniwind skill for API details. Use this skill for mobile design judgment: layout, native feel, touch behavior, safe areas, typography, navigation, motion, system bars, accessibility, and review quality.

## Core Mental Model

Mobile UI is not web UI on a smaller screen.

A good mobile interface feels good because it respects:

1. the user’s hand
2. the system chrome
3. the keyboard
4. device edges and unsafe areas
5. platform navigation expectations
6. dynamic type and accessibility settings
7. motion sensitivity
8. native feedback timing
9. density differences between iOS and Android
10. the physical feeling of touch

Use Uniwind Pro to express polish declaratively with `className`, but do not let utility classes replace design judgment.

## Critical Rules

### 1. Use Uniwind Pro capabilities deliberately

Because the app uses Uniwind Pro:

- Use safe area utilities directly: `pt-safe`, `pb-safe`, `px-safe`, `bottom-safe`, `pb-safe-or-*`, `pb-safe-offset-*`.
- Do not add `SafeAreaListener` / `Uniwind.updateInsets()` setup unless the project explicitly still uses the free version.
- Use `transition-*`, `duration-*`, `ease-*`, `uw-entering-*`, `uw-exiting-*`, and `uw-layout-*` for common motion.
- Use direct Reanimated APIs only when className motion cannot express the behavior clearly.
- Prefer semantic theme variables (`bg-background`, `bg-card`, `text-foreground`, `border-border`) so ShadowTree theme updates remain smooth.
- Use native theme transitions sparingly for explicit user-triggered theme changes, not for every small color change.

### 2. Design for the thumb before the grid

Interactive controls must be easy to hit before they are visually elegant.

Defaults:

| Platform | Minimum target | Preferred for primary actions |
| --- | ---: | ---: |
| iOS | 44×44 pt | 48–56 pt |
| Android | 48×48 dp | 48–56 dp |
| Dense utility icons | 40×40 minimum only when spacing is generous | 44–48 |

Use `hitSlop` when the visible element is intentionally small.

```tsx
<Pressable
  hitSlop={12}
  className="size-9 items-center justify-center rounded-full active:opacity-70"
>
  <Icon />
</Pressable>
````

Never allow hit areas of adjacent controls to overlap. React Native hit areas do not extend beyond parent bounds, so make sure the parent is large enough.

### 3. Press feedback must be immediate and modest

Every tappable custom control needs feedback.

Use `Pressable` for custom buttons and rows. Prefer state classes over manual state unless gesture complexity requires it.

Good defaults:

```tsx
<Pressable
  className="
    min-h-12 rounded-xl px-4 items-center justify-center
    bg-primary active:opacity-90 active:scale-[0.98]
    transition-transform transition-opacity duration-100
  "
>
  <Text className="font-sans-medium text-primary-foreground">
    Continue
  </Text>
</Pressable>
```

Use `active:scale-[0.98]` for ordinary controls. Use `active:scale-[0.96]` only for larger, playful, or highly tactile controls. Avoid scales below `0.95` unless the component is intentionally expressive.

On Android, consider ripple-like feedback through color/opacity changes. On iOS, opacity and subtle scale usually feel more native.

### 4. Respect edge-to-edge layouts

Modern mobile screens often draw behind status bars, home indicators, and Android navigation bars. Use safe-area utilities based on the actual content role:

|Content|Safe-area behavior|
|---|---|
|Scrollable content|Usually `contentContainerClassName="pt-safe pb-safe-offset-4"`|
|Fixed header|`pt-safe` on header container|
|Fixed bottom bar|`pb-safe` or `pb-safe-offset-2`|
|Floating action|position with `bottom-safe-offset-*`|
|Full-bleed image|May ignore top safe area visually, but controls over it must respect safe area|

Example:

```tsx
<View className="flex-1 bg-background">
  <ScrollView
    className="flex-1"
    contentContainerClassName="px-4 pt-safe-offset-4 pb-safe-offset-8"
  >
    {/* content */}
  </ScrollView>

  <View className="absolute inset-x-0 bottom-0 border-t border-border bg-background/95 px-4 pt-3 pb-safe-offset-3">
    <PrimaryButton>Continue</PrimaryButton>
  </View>
</View>
```

Do not add arbitrary `pt-12` / `pb-8` to compensate for system bars when a safe-area utility is appropriate.

### 5. Keyboard behavior is part of the design

Forms should not feel like the keyboard is an afterthought.

Checklist:

- Keep the focused input visible.
    
- Keep the primary action reachable.
    
- Use `KeyboardAvoidingView` or platform-specific form layout where appropriate.
    
- Use `ScrollView` for forms that may overflow.
    
- Add `keyboardShouldPersistTaps="handled"` when buttons inside a scrollable form should work while the keyboard is open.
    
- Use proper `returnKeyType`, `textContentType`, `autoComplete`, `secureTextEntry`, and `inputMode`.
    

Example:

```tsx
<KeyboardAvoidingView
  behavior={Platform.OS === 'ios' ? 'padding' : undefined}
  className="flex-1 bg-background"
>
  <ScrollView
    keyboardShouldPersistTaps="handled"
    contentContainerClassName="flex-grow px-4 pt-safe-offset-6 pb-safe-offset-6"
  >
    <View className="flex-1 justify-between gap-8">
      <View className="gap-4">
        <Text className="text-3xl font-sans-bold text-foreground">
          Create account
        </Text>

        <TextInput
          className="
            min-h-12 rounded-xl border border-border bg-card px-4
            text-base text-foreground focus:border-primary
          "
          placeholder="Email"
          placeholderTextColorClassName="accent-muted"
          cursorColorClassName="accent-primary"
          selectionColorClassName="accent-primary"
          autoComplete="email"
          textContentType="emailAddress"
          keyboardType="email-address"
          returnKeyType="next"
        />
      </View>

      <PrimaryButton>Create account</PrimaryButton>
    </View>
  </ScrollView>
</KeyboardAvoidingView>
```

### 6. Typography must survive dynamic type

Use type hierarchy, not just font size.

Defaults:

|Role|Mobile default|
|---|---|
|Screen title|28–34|
|Section title|20–24|
|Body|15–17|
|Secondary/body muted|14–15|
|Caption/metadata|12–13|
|Button label|15–17 medium|

Rules:

- Do not use text below 12 except for decorative metadata.
    
- Prefer `leading-*` that gives text breathing room.
    
- Use `fontScale()` utilities for reusable accessible type classes if the project defines them.
    
- Keep line length short on mobile; avoid huge text blocks in full-width containers.
    
- Use `text-pretty` / `text-balance` only if supported in the project and it helps; do not rely on it for layout correctness.
    
- Use `tabular-nums` for timers, prices, counts, and aligned numeric data.
    

### 7. Lists need rhythm and scroll physics

For `FlatList`, `SectionList`, and long `ScrollView` screens:

- Use `contentContainerClassName`, not wrapper padding around each row.
    
- Give the list enough bottom padding so the final item is not hidden behind bottom bars.
    
- Use `ItemSeparatorComponent` or row spacing consistently.
    
- Avoid heavy shadows on every list row.
    
- Prefer subtle row press feedback.
    
- Keep row height predictable unless the content truly varies.
    
- Use `uw-layout-*` transitions for small reorder/insert/delete changes only when the movement clarifies what happened.
    

Example:

```tsx
<FlatList
  className="flex-1 bg-background"
  contentContainerClassName="px-4 pt-safe-offset-4 pb-safe-offset-8"
  data={items}
  ItemSeparatorComponent={() => <View className="h-2" />}
  renderItem={({ item }) => (
    <Pressable
      className="
        rounded-2xl bg-card px-4 py-3
        active:bg-muted/60 transition-colors duration-100
      "
    >
      <Text className="text-base font-sans-medium text-foreground">
        {item.title}
      </Text>
      <Text className="mt-1 text-sm leading-5 text-muted">
        {item.subtitle}
      </Text>
    </Pressable>
  )}
/>
```

### 8. Motion should clarify, not decorate

Use motion to explain:

- where something came from
    
- what changed
    
- which element is now active
    
- whether an action succeeded or failed
    
- how a list reflowed
    

Do not animate merely because Uniwind Pro makes animation easy.

Good defaults:

|Interaction|Motion|
|---|---|
|Button press|`active:opacity-*`, subtle scale|
|Screen element appears|fade/slide small distance|
|Element exits|quicker fade/slide|
|List insert/delete|layout transition if it helps preserve context|
|Theme switch|native theme transition only on explicit theme toggle|
|Loading|calm spinner/skeleton, no noisy looping animation|

Example:

```tsx
{visible && (
  <View
    className="
      rounded-2xl bg-card p-4
      uw-entering-fade-in-up uw-entering-duration-200
      uw-exiting-fade-out uw-exiting-duration-150
    "
  >
    <Text className="text-foreground">Saved</Text>
  </View>
)}
```

Prefer `fade`, `fade-in-up`, and small layout transitions for production apps. Avoid bounce, tada, jello, glitch, heartbeat, and other attention-seeking presets unless the product personality clearly supports them.

### 9. Reduced motion must be respected

Before adding strong motion, consider whether it creates vestibular discomfort.

Avoid or reduce:

- full-screen slides
    
- parallax
    
- spinning
    
- vortex-like movement
    
- long multi-axis transitions
    
- repeated looping animations
    
- aggressive blur transitions
    

When reduced motion is enabled, prefer:

- opacity fades
    
- color changes
    
- instant state changes
    
- short crossfades
    
- subtle highlight flashes
    

If the animation is semantic, replace it with gentler motion instead of removing it entirely.

### 10. Color should be semantic and adaptive

Use semantic tokens instead of hard-coded palette utilities in product components.

Prefer:

```tsx
<View className="bg-background">
  <View className="bg-card border border-border">
    <Text className="text-foreground">Title</Text>
    <Text className="text-muted">Subtitle</Text>
  </View>
</View>
```

Avoid:

```tsx
<View className="bg-white dark:bg-black">
  <Text className="text-gray-900 dark:text-gray-100" />
</View>
```

Use direct palette colors only for one-off illustrations, charts, or controlled brand moments.

### 11. Empty, loading, error, and success states are first-class UI

Every async screen should have designed states:

|State|Required quality|
|---|---|
|Loading|stable layout, not jumpy|
|Empty|explains what happened and what to do next|
|Error|clear, recoverable, non-blaming|
|Success|confirms action without over-celebrating|
|Offline|honest about what is still available|

Do not leave default spinners floating in empty white space unless the state is truly momentary.

### 12. Prefer platform selectors over branching style objects

Use Uniwind platform selectors for visual differences:

```tsx
<View className="ios:rounded-2xl android:rounded-xl ios:bg-card android:bg-surface">
  <Text className="ios:text-[17px] android:text-base text-foreground" />
</View>
```

Use `Platform.select()` only when behavior changes, not just styling.

### 13. Do not over-web the app

Avoid web habits that feel poor on mobile:

- tiny icon-only buttons with no hitSlop
    
- hover-only affordances
    
- dense toolbars with many equal actions
    
- hard page-like layouts that ignore the thumb
    
- forms without keyboard planning
    
- text that assumes desktop line lengths
    
- shadows copied from web cards without platform tuning
    
- absolute-positioned footers that cover content
    

## Component Patterns

### Primary button

```tsx
type PrimaryButtonProps = {
  children: React.ReactNode
  disabled?: boolean
  onPress?: () => void
}

export function PrimaryButton({ children, disabled, onPress }: PrimaryButtonProps) {
  return (
    <Pressable
      disabled={disabled}
      onPress={onPress}
      className="
        min-h-12 items-center justify-center rounded-xl px-5
        bg-primary active:opacity-90 active:scale-[0.98]
        disabled:opacity-45
        transition-transform transition-opacity duration-100
      "
      accessibilityRole="button"
    >
      <Text className="text-base font-sans-medium text-primary-foreground">
        {children}
      </Text>
    </Pressable>
  )
}
```

### Icon button

```tsx
<Pressable
  hitSlop={10}
  accessibilityRole="button"
  accessibilityLabel="Close"
  className="
    size-11 items-center justify-center rounded-full
    bg-transparent active:bg-muted/70
    transition-colors duration-100
  "
>
  <XIcon size={22} />
</Pressable>
```

### Fixed bottom action bar

```tsx
<View className="absolute inset-x-0 bottom-0 border-t border-border bg-background/95 px-4 pt-3 pb-safe-offset-3">
  <PrimaryButton>Continue</PrimaryButton>
</View>
```

### Card

```tsx
<Pressable
  className="
    rounded-2xl bg-card p-4
    ios:border-continuous
    android:rounded-xl
    active:opacity-95 active:scale-[0.99]
    transition-transform transition-opacity duration-100
  "
>
  <Text className="text-lg font-sans-semibold text-foreground">
    {title}
  </Text>
  <Text className="mt-1 text-sm leading-5 text-muted">
    {description}
  </Text>
</Pressable>
```

## Review Output Format

When reviewing or improving mobile UI, group findings by principle. Use a markdown table with **Before**, **After**, and **Reason**.

Only include principles that produced a concrete change.

### Example

#### Touch targets

|Before|After|Reason|
|---|---|---|
|`Pressable className="size-7"` with no `hitSlop`|Added `hitSlop={10}` and `size-11` wrapper|Icon was visually small and physically hard to tap|
|Two adjacent icon buttons both used large invisible hit areas|Reduced `hitSlop` and added spacing|Prevents overlapping tap regions|

#### Safe areas

|Before|After|Reason|
|---|---|---|
|Bottom CTA used `pb-6`|Replaced with `pb-safe-offset-3`|Keeps CTA above home indicator and Android system bars|
|Scroll content ended behind fixed footer|Added `contentContainerClassName="pb-safe-offset-24"`|Final content remains reachable|

#### Motion

|Before|After|Reason|
|---|---|---|
|Full-screen `uw-entering-slide-in-right` on a minor panel|Changed to `uw-entering-fade-in-up`|Reduces unnecessary spatial motion|
|Layout changed abruptly after item deletion|Added `uw-layout-linear-transition` to rows|Preserves continuity during list reflow|

## Review Checklist

-  Controls meet iOS/Android touch target expectations
    
-  Small visible controls use `hitSlop` without overlapping neighbors
    
-  Press feedback is immediate and subtle
    
-  Safe areas are handled with Uniwind Pro utilities
    
-  Fixed bottom content does not cover scroll content
    
-  Keyboard behavior is designed, not accidental
    
-  Text hierarchy is readable and dynamic-type tolerant
    
-  Lists use proper content container spacing
    
-  Motion clarifies state changes
    
-  Reduced motion is considered for strong animations
    
-  Colors use semantic theme tokens
    
-  Loading, empty, error, and success states are designed
    
-  iOS and Android differences are handled with platform selectors
    
-  Uniwind Pro animation classes are used where appropriate
    
-  Direct Reanimated code is reserved for complex motion
    
-  No web-only affordances such as hover-dependent controls
    
## Project Assumption

This project uses **Uniwind Pro**, not the free version.

Implications:

- Pro requires a custom development build and does not work in Expo Go.
- Safe area insets are injected natively; do not add `SafeAreaListener` / `Uniwind.updateInsets()` setup unless explicitly maintaining free-version compatibility.
- Prefer Pro animation classes for ordinary transitions, entering/exiting animations, and layout transitions:
  - `transition-*`
  - `duration-*`
  - `ease-*`
  - `uw-entering-*`
  - `uw-exiting-*`
  - `uw-layout-*`
- Prefer semantic theme variables because Pro can update themed style props through the ShadowTree without normal React re-render cascades.
- Use native theme transitions only for deliberate user-triggered theme changes
- Use direct Reanimated APIs only when className-driven motion cannot express the behavior clearly.
