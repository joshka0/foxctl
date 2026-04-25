---
name: uniwind-rn-ios
description: "Design or review Expo and React Native iOS UI with Uniwind Pro, tabs, nav bars, sheets, blur, safe areas, and Dynamic Type."
---

# iOS Native Feel — Expo + React Native + Uniwind Pro

Make React Native screens feel like they belong on iPhone and iPad without blindly copying Apple system apps.

Use this skill when a UI should feel more native on iOS.

## Core iOS Qualities

Great iOS interfaces tend to feel:

- content-first
- quiet
- spatially clear
- generous with touch
- precise in typography
- subtle in motion
- respectful of safe areas
- light on decorative borders
- careful with hierarchy
- comfortable one-handed

Do not make iOS screens feel like web dashboards. Avoid dense chrome, over-boxed layouts, tiny controls, and excessive shadows.

## iOS Rules

### 1. Minimum hit target: 44×44 pt

All primary tappable controls should meet or exceed 44×44.

Use larger targets for:

- destructive actions
- frequently repeated actions
- controls near screen edges
- icon-only buttons
- bottom actions
- controls used while walking or multitasking

Good iOS icon button:

```tsx
<Pressable
  hitSlop={8}
  accessibilityRole="button"
  accessibilityLabel="Close"
  className="
    size-11 items-center justify-center rounded-full
    ios:active:bg-muted/60
    transition-colors duration-100
  "
>
  <XIcon size={22} />
</Pressable>
```

### 2. Respect the home indicator and status area

Use Uniwind Pro safe area utilities.

Good:

```tsx
<View className="flex-1 bg-background">
  <ScrollView contentContainerClassName="px-5 pt-safe-offset-4 pb-safe-offset-8">
    {/* content */}
  </ScrollView>
</View>
```

Fixed bottom action:

```tsx
<View className="absolute inset-x-0 bottom-0 bg-background/95 px-5 pt-3 pb-safe-offset-3">
  <PrimaryButton>Continue</PrimaryButton>
</View>
```

Avoid hard-coded bottom padding like `pb-8` for iPhone home indicator spacing.

### 3. Use iOS typography proportions

Good defaults:

|Role|Size|Weight|
|---|--:|---|
|Large screen title|32–34|bold|
|Screen title|28–30|bold / semibold|
|Section heading|20–22|semibold|
|Body|16–17|regular|
|Secondary|14–15|regular|
|Caption|12–13|regular / medium|
|Button|16–17|semibold|

Prefer a slightly larger body size on iOS than Android when using platform variants:

```tsx
<Text className="ios:text-[17px] android:text-base leading-6 text-foreground">
  {body}
</Text>
```

### 4. Use large titles only for true top-level screens

A large title works well when:

- the screen is a top-level destination
    
- the content below is a list, feed, settings, library, or overview
    
- the title names a place, not an action
    

Avoid large titles for:

- modals
    
- dense forms
    
- detail screens with a strong object title
    
- short-lived confirmation screens
    

Pattern:

```tsx
<View className="px-5 pt-safe-offset-4 pb-3">
  <Text className="text-[34px] leading-10 font-sans-bold text-foreground">
    Library
  </Text>
</View>
```

### 5. iOS cards should feel soft, not heavy

Use rounded corners, subtle separation, and restrained shadows.

Good card:

```tsx
<View
  className="
    rounded-2xl border-continuous bg-card p-4
    shadow-sm
  "
>
  <Text className="text-[17px] font-sans-semibold text-foreground">
    {title}
  </Text>
  <Text className="mt-1 text-[15px] leading-5 text-muted">
    {subtitle}
  </Text>
</View>
```

Prefer:

- `rounded-2xl`
    
- `border-continuous`
    
- soft backgrounds
    
- subtle hairline dividers
    
- light shadows only for elevated objects
    

Avoid:

- heavy Android-like elevation shadows
    
- boxed layouts around every row
    
- thick borders
    
- dense grid systems
    

### 6. Use grouped-list structure for settings-like screens

iOS settings screens often feel better as grouped sections than as independent cards.

Pattern:

```tsx
<View className="gap-7 px-5 pt-safe-offset-4 pb-safe-offset-8">
  <View className="gap-2">
    <Text className="px-1 text-xs font-sans-medium uppercase tracking-wide text-muted">
      Account
    </Text>

    <View className="overflow-hidden rounded-2xl border-continuous bg-card">
      <SettingsRow label="Profile" />
      <View className="ml-4 h-hairline bg-border" />
      <SettingsRow label="Security" />
    </View>
  </View>
</View>
```

Rows should usually be at least 44 high, preferably 48–56 for comfort.

### 7. Prefer quiet iOS press states

For iOS:

- opacity change is acceptable
    
- subtle background fill on list rows is acceptable
    
- tiny scale is acceptable for buttons/cards
    
- avoid loud ripples
    
- avoid intense color flashes
    

```tsx
<Pressable
  className="
    min-h-12 flex-row items-center justify-between px-4
    active:bg-muted/50 transition-colors duration-100
  "
>
  <Text className="text-[17px] text-foreground">{label}</Text>
  <ChevronRightIcon />
</Pressable>
```

### 8. Use blur carefully

Blur can feel very iOS, but it is easy to overuse.

Use blur for:

- translucent navigation areas
    
- bottom bars over scrolling content
    
- modals/sheets where background context matters
    

Avoid blur for:

- ordinary cards
    
- dense lists
    
- text-heavy surfaces
    
- performance-sensitive repeated elements
    

If blur is unavailable or too costly, use `bg-background/95` with a subtle border.

### 9. Sheets should feel anchored and safe

For bottom sheets or modal sheets:

- use rounded top corners
    
- keep drag handles subtle
    
- respect `pb-safe`
    
- do not put destructive actions too close to the home indicator
    
- make dismissal affordances clear
    
- avoid nested sheets unless unavoidable
    

Pattern:

```tsx
<View className="absolute inset-x-0 bottom-0 rounded-t-3xl bg-background px-5 pt-3 pb-safe-offset-5">
  <View className="mx-auto mb-4 h-1 w-9 rounded-full bg-muted/50" />

  <Text className="text-xl font-sans-semibold text-foreground">
    Choose plan
  </Text>

  <View className="mt-5 gap-3">
    {/* options */}
  </View>
</View>
```

### 10. iOS motion should be spatial but gentle

Good iOS motion:

- short
    
- interruptible
    
- directional when hierarchy changes
    
- mostly opacity/translate/scale
    
- never gratuitously bouncy
    

For small elements:

```tsx
<View
  className="
    uw-entering-fade-in-up uw-entering-duration-200
    uw-exiting-fade-out uw-exiting-duration-150
  "
/>
```

Avoid expressive presets like `bounce`, `jello`, `tada`, and `glitch` unless the brand is playful and the moment is low-stakes.

### 11. Respect reduced motion and transparency preferences

When motion or blur is ornamental, provide a calmer fallback.

If reduced motion is enabled:

- replace large slides with fade
    
- remove parallax
    
- stop looping animations
    
- avoid animated blur/depth effects
    

If reduced transparency is enabled:

- avoid relying on blur
    
- use opaque backgrounds
    
- preserve contrast
    

### 12. Use platform selectors, not global compromise

Do not force a single design to serve iOS and Android equally.

```tsx
<View
  className="
    ios:rounded-2xl ios:border-continuous
    android:rounded-xl
    bg-card
  "
/>
```

## iOS Anti-patterns

Avoid:

- 36px-high buttons
    
- bottom CTAs without `pb-safe`
    
- web-like hover assumptions
    
- dense icon clusters in the top bar
    
- heavy shadows on every card
    
- Android-style ripples
    
- using the same typography scale as Android without checking optical fit
    
- putting destructive controls next to the home indicator
    
- hiding key actions behind tiny glyphs
    
- full-screen motion for minor state changes
    

## iOS Review Output Format

Use markdown tables grouped by principle.

### Example

#### iOS touch targets

|Before|After|Reason|
|---|---|---|
|`size-8` close button|`size-11` plus `hitSlop={8}`|Meets iOS 44pt target and is easier to tap|
|Bottom CTA used visual height 40|Changed to `min-h-12`|Primary action needs comfortable thumb target|

#### iOS safe areas

|Before|After|Reason|
|---|---|---|
|Footer used `pb-4`|`pb-safe-offset-3`|Keeps action clear of home indicator|
|Header used fixed `pt-12`|`pt-safe-offset-4`|Adapts across iPhone models|

#### iOS visual feel

|Before|After|Reason|
|---|---|---|
|Cards used heavy shadow|Reduced to subtle shadow and `border-continuous`|Better matches iOS softness|
|Settings rows were separate cards|Grouped rows inside one rounded section|Feels more like native iOS settings|
