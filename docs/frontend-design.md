# AutoLinks — Frontend Design System

> Adapted from Intercom's editorial design language. The warm cream canvas, white floating cards, and charcoal ink system are the defining moves; AutoLinks inherits those and adds a dark mode complement and a gold product accent in place of Fin Orange.

---

## Overview

AutoLinks sits on a **warm cream canvas** (`--canvas` ≈ #f5f1ec) — not pure white, not cool gray. The warmth signals editorial calm, and every UI decision reinforces that tone. Floating white cards lift off the cream to create hierarchy without shadows. Charcoal ink (`--ink` #111111) handles all type and primary actions. A single chromatic accent — **Link Gold** (`--ac` #b8911f) — marks entity highlights, active recommendation cards, and matched phrase pulses.

Dark mode inverts the warmth rather than flipping to neutral black: the canvas becomes a very dark warm brown (`#18140f`) and the primary text color becomes the light cream (`#f5f1ec`), preserving the material feel across both modes.

**Key characteristics:**
- Cream canvas is the brand's defining surface — never pure white, never cool gray.
- White floating panels lift off cream for depth. No drop shadows.
- Charcoal (`--ink`) is the system primary — buttons, headlines, labels.
- Link Gold is a product accent — used only for highlights, active states, and matched entities.
- Weight 500 on display and labels; weight 400 on body. Negative letter-spacing scales with size.
- Dark mode uses warm dark browns, not neutral blacks.

---

## Colors

### Light Mode

| Token | Value | Usage |
|-------|-------|-------|
| `--canvas` | `#f5f1ec` | Page background — the defining warm cream |
| `--sf` (surface-1) | `#ffffff` | Floating panels, cards, editor area |
| `--sf2` (surface-2) | `#ede9e2` | Hover states, secondary surfaces |
| `--ink` | `#111111` | Headlines, body, primary button background |
| `--ink-m` (ink-muted) | `#626260` | Labels, secondary text, dropdown items |
| `--ink-s` (ink-subtle) | `#7b7b78` | Section labels, meta, timestamps |
| `--hl` (hairline) | `#d3cec6` | Card borders, panel dividers |
| `--hl-s` (hairline-soft) | `#e4e0d9` | Editor borders, subtle dividers |
| `--ac` (link-gold) | `#b8911f` | Active card accent, matched phrase highlight |
| `--ac-hl` (gold-tint) | `#fdf4de` | Highlight background in editor, active card fill |
| `--gm` (score-match) | `#1a7a3c` | Match score text |
| `--eq` (score-equity) | `#6d28d9` | Equity score text |

### Dark Mode

| Token | Value | Usage |
|-------|-------|-------|
| `--canvas` | `#18140f` | Very dark warm brown — page background |
| `--sf` | `#231e17` | Dark warm card surface |
| `--sf2` | `#2e2720` | Elevated hover surface |
| `--ink` | `#f5f1ec` | Cream text — mirrors light mode canvas |
| `--ink-m` | `#a09890` | Muted text |
| `--ink-s` | `#6e6458` | Subtle / disabled text |
| `--hl` | `#3d3529` | Card borders, dividers |
| `--hl-s` | `#2a2318` | Soft dividers, editor border |
| `--ac` | `#d4a843` | Link Gold — slightly brighter in dark context |
| `--ac-hl` | `#2e2208` | Dark amber tint for highlights |
| `--gm` | `#34a85a` | Match score — brighter for dark contrast |
| `--eq` | `#a78bfa` | Equity score — brighter for dark contrast |

### Palette Principles

- **Never use pure white as the page canvas.** Surface-1 white appears only on lifted cards and panels.
- **Link Gold is a product accent only** — entity highlights, active card left-border, and phrase pulse animation. Not a section background, not a generic CTA color.
- **Depth comes from surface change, not shadows.** White-on-cream (light) or dark-card-on-darker-canvas (dark) is the only elevation mechanism.
- **Match and equity scores use semantic green and purple.** These are data colors, not brand colors.

---

## Typography

### Font Families

| Role | Family | Fallback |
|------|--------|---------|
| Display, UI | `Instrument Sans` | `system-ui, -apple-system, 'Segoe UI', sans-serif` |
| Editor / body prose | `Merriweather` | `Georgia, 'Times New Roman', serif` |
| Scores / metrics | `JetBrains Mono` | `'Courier New', monospace` |

The sans family carries all chrome (header, labels, buttons, cards, dropdowns). The serif family is used only inside the draft editor — it signals "writing surface." Mono is used only for match and equity scores.

### Scale

| Token | Size | Weight | Line Height | Letter Spacing | Use |
|-------|------|--------|-------------|----------------|-----|
| `--t-logo` | 17px | 500 | 1.0 | -0.04em | AutoLinks wordmark |
| `--t-label` | 10px | 500 | 1.3 | 0.06em | Section labels (uppercase) |
| `--t-body` | 14px | 400 | 1.5 | 0 | Card body, dropdown items |
| `--t-body-sm` | 12px | 400 | 1.5 | 0 | Card context, meta |
| `--t-card-title` | 12px | 500 | 1.3 | -0.01em | Recommendation phrase |
| `--t-score` | 11px | 400 | 1.0 | 0 | Match / equity scores (monospace) |
| `--t-url` | 11px | 500 | 1.3 | 0.01em | Suggested URL in cards |
| `--t-btn` | 12px | 500 | 1.2 | 0.01em | Analyze button |
| `--t-editor` | 14px | 400 | 1.85 | 0 | Draft textarea (serif) |
| `--t-tagline` | 11px | 400 | 1.0 | 0.01em | Header tagline |

### Principles

- **Weight 500 for structure, 400 for content.** Labels, card titles, the logo, and the button all run at 500. Editor text, card context, and meta run at 400.
- **Negative letter-spacing only on display.** The logo pulls -0.04em; card titles pull -0.01em; body runs at 0.
- **Section labels are uppercase + small + subtle.** 10px / 500 / 0.06em tracking / `--ink-s` color. Never sentence-case eyebrows in the app chrome.
- **Serif in the editor only.** Merriweather signals the writing context and nothing else.

---

## Spacing

Base unit: 8px.

| Token | Value | Use |
|-------|-------|-----|
| `--space-xs` | 4px | Internal chip gaps |
| `--space-sm` | 8px | Inline label gaps |
| `--space-md` | 12px | Between cards, row gaps |
| `--space-lg` | 18px | Panel inner padding (top/sides) |
| `--space-xl` | 24px | Header padding |
| `--space-btn-v` | 8px | Button vertical padding |
| `--space-btn-h` | 18px | Button horizontal padding |
| `--space-card-v` | 12px | Card vertical padding |
| `--space-card-h` | 14px | Card horizontal padding |
| `--space-editor` | 14px | Editor inner padding |

---

## Border Radius

| Token | Value | Use |
|-------|-------|-----|
| `--r-xs` | 4px | Badges, score pills |
| `--r-sm` | 6px | Dropdown items hover |
| `--r-md` | 8px | Buttons, form inputs |
| `--r-lg` | 10px | Recommendation cards |
| `--r-xl` | 12px | Editor textarea |
| `--r-app` | 14px | App container |
| `--r-full` | 9999px | Avatar circle |

No pill-rounded buttons. No square buttons. Buttons sit at `--r-md` 8px.

---

## Elevation

Depth is communicated by **surface color change**, never by drop shadows.

| Level | Treatment | Use |
|-------|-----------|-----|
| 0 — canvas | `--canvas` background | Page background, header bar |
| 1 — lift | `--sf` white / dark-card on canvas | Panels, cards, editor |
| 2 — accent lift | `--ac-hl` gold tint + `inset 3px 0 0 --ac` | Active recommendation card |

**No `box-shadow` with spread or offset.** The only permitted shadow is `box-shadow: inset 3px 0 0 var(--ac)` on the active card left accent.

---

## Components

### Header

```
background: --canvas
border-bottom: 1px solid --hl
height: 52px
padding: 12px 20px
```

- Logo left: `Instrument Sans` 17px / 500 / letter-spacing -0.04em / `--ink`
- Tagline center: 11px / 400 / `--ink-s` / flex-grow
- Avatar right: 28px circle / `--ink` background / `--canvas` text / `--r-full`

### User Dropdown

```
background: --sf
border: 1px solid --hl
border-radius: --r-lg (10px)
padding: 8px
min-width: 180px
position: absolute — right-aligned to avatar
```

- Theme buttons: flex row, `--canvas` background, `--hl` border, `--r-sm`. Selected state: `--sf` background, `--ink` text, weight 500.
- Menu items: 12px / `--ink-m`, hover `--sf2` background, `--r-sm`.
- Separator: 1px `--hl-s`.

### Draft Editor Panel

```
flex: 3
border-right: 1px solid --hl
```

- Section label: `--t-label` / uppercase / `--ink-s`
- Editor area: `--sf` background / `--r-xl` border / 1px `--hl-s` border / `--t-editor` (Merriweather) / `--ink` text / min-height 248px
- Editor focus: border-color → `--hl` (does not use accent color — the editor is a neutral writing surface)

### Analyze Button

```
background: --ink
color: --canvas
font: --t-btn (Instrument Sans 12px / 500)
padding: --space-btn-v --space-btn-h
border-radius: --r-md
border: none
```

Loading state: inline spinner (1.5px border, `--hl` track, `--ink-s` active arc) + "Analyzing…" text. Disabled opacity: 0.4.

### Recommendation Card

```
background: --sf
border: 1px solid --hl-s
border-radius: --r-lg (10px)
padding: --space-card-v --space-card-h
```

**Normal:**
- Border: 1px `--hl-s`
- Background: `--sf`

**Hover:**
- Border: 1px `--hl`
- Background: `--canvas`

**Active (clicked):**
- Background: `--ac-hl`
- Border: 1px `--hl`
- `box-shadow: inset 3px 0 0 var(--ac)`

Card anatomy (top to bottom):
1. Phrase title — `--t-card-title` / `--ink` / -0.01em tracking
2. Context snippet — 12px / Merriweather / italic / `--ink-m` / 1.55 line-height
3. URL link — `--t-url` / `--ac` / weight 500 / ↗ prefix
4. Scores row — `--t-score` / JetBrains Mono: match in `--gm`, equity in `--eq`

### Entity Highlight (in editor)

```html
<mark class="hl" data-phrase-key="...">phrase</mark>
```

```
background: --ac-hl
border-radius: 3px
padding: 1px 3px
color: --ink
transition: background 0.35s, color 0.35s
```

Pulse state (on card click):
```
background: --ac
color: #ffffff
```
Duration: 1400ms, then reverts via CSS transition.

### Recommendations Panel

```
flex: 2
padding: --space-lg
```

States:
- **Empty:** centered text, `--ink-s`, 13px, 44px top padding
- **Loading:** spinner + "Analyzing draft…" in same style
- **Populated:** stacked `al-card` components, 8px gap

---

## Bidirectional Highlight Interaction

1. User clicks a recommendation card.
2. Card gains `.on` class → `--ac-hl` background + `inset 3px 0 0 --ac` left accent.
3. Corresponding `<mark>` in editor gains `.pulse` class → background transitions to `--ac`, text to white.
4. After 1400ms, `.pulse` is removed → transitions back to `--ac-hl` tint.
5. Only one card is active at a time. Clicking another card clears the previous `.on` state.

---

## Theme System

Three modes: `light`, `dark`, `system`.

- `system` reads `window.matchMedia('(prefers-color-scheme: dark)')`.
- Applied via `data-theme="dark"` on the root `#al` element.
- All color tokens are CSS custom properties scoped to `#al` and `#al[data-theme="dark"]`.
- Theme preference persisted to `localStorage` under key `autolinks-theme`.
- No transition animation on theme switch (avoids flicker on page load).

```css
#al { --canvas: #f5f1ec; /* ... light tokens */ }
#al[data-theme="dark"] { --canvas: #18140f; /* ... dark tokens */ }
```

---

## Layout

### Two-Panel Grid

```
Desktop (≥1024px): flex row
  Left panel (.al-ep):  flex: 3  (≈60%)
  Right panel (.al-rp): flex: 2  (≈40%)
  Divider: 1px solid --hl

Mobile (<1024px): flex column
  Panels stack vertically
  Divider becomes border-bottom
```

### App Container

```
background: --canvas
border: 1px solid --hl
border-radius: --r-app (14px)
overflow: hidden
```

The container border uses `--hl` (#d3cec6 light / #3d3529 dark) — warm, never cool.

---

## Do's and Don'ts

### Do

- Use `--canvas` cream as the default background. Never replace with pure white.
- Lift the editor and cards onto `--sf` white for hierarchy.
- Use **Link Gold** only for entity highlights, active card left accent, and phrase pulse.
- Separate display weight (500) from body weight (400).
- Use the warm dark palette in dark mode — `#18140f` canvas, not `#000000`.
- Apply negative letter-spacing to the logo and card titles only.
- Use Merriweather exclusively inside the editor textarea.

### Don't

- Don't add `box-shadow` with offset or blur anywhere.
- Don't use Link Gold as a button background or section fill.
- Don't use all-caps on anything except section labels.
- Don't pill-round the Analyze button.
- Don't introduce a second sans-serif family for chrome.
- Don't show the editor's focus ring in the accent color — keep it at `--hl`.
- Don't use pure black (#000) or pure white (#fff) as backgrounds in either mode.
- Don't use the match/equity colors (green, purple) anywhere outside score display.

---

## CSS Variable Reference

```css
/* Light mode (default) */
#al {
  --canvas:  #f5f1ec;
  --sf:      #ffffff;
  --sf2:     #ede9e2;
  --ink:     #111111;
  --ink-m:   #626260;
  --ink-s:   #7b7b78;
  --hl:      #d3cec6;
  --hl-s:    #e4e0d9;
  --ac:      #b8911f;
  --ac-hl:   #fdf4de;
  --gm:      #1a7a3c;
  --eq:      #6d28d9;
}

/* Dark mode */
#al[data-theme="dark"] {
  --canvas:  #18140f;
  --sf:      #231e17;
  --sf2:     #2e2720;
  --ink:     #f5f1ec;
  --ink-m:   #a09890;
  --ink-s:   #6e6458;
  --hl:      #3d3529;
  --hl-s:    #2a2318;
  --ac:      #d4a843;
  --ac-hl:   #2e2208;
  --gm:      #34a85a;
  --eq:      #a78bfa;
}
```