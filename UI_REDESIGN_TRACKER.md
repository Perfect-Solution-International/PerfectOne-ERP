# PerfectOne ERP — UI Redesign Tracker

Source of truth for the liquid-glass redesign. Routes below were discovered directly from
`apps/web/app/**/page.tsx` (not guessed, not sidebar-only). Re-run
`find apps/web/app -name page.tsx` from the repo root to re-verify this list is still complete.

---

## Pass 2 — liquid glass design system + full responsive rebuild (2026-09-12)

### What changed and why

The previous pass had accumulated as a long tail of per-page `!important` patches inside
`globals.css` (173 KB, ~980 lines, most of them minified). The result read as flat near-black
panels under a heavy red fog, with 9–10px labels — legible, but tiring to look at for a full
shift, and inconsistent from page to page.

Pass 2 replaces that with **one design layer**: `apps/web/app/liquid-glass.css`, imported from
`layout.tsx` immediately after `globals.css` so it is the final authority on material, colour,
depth and layout. `globals.css` was left in place; nothing was deleted from it.

**Read `liquid-glass.css` top to bottom before changing any styling** — it is ordered, commented,
and each section says what problem it solves.

| # | Section | What it owns |
|---|---|---|
| 1 | Tokens | light + dark palettes, the glass material variables, radii, shadows |
| 2 | Base + ambient | the page-level aurora, typography, scrollbars, focus rings |
| 3 | The glass surface | one material for every panel, plus nested-glass de-escalation |
| 4–6 | Sidebar / topbar / page frame | the shell |
| 7–8 | Buttons / inputs | glass pills, brand fill for primaries, recessed input wells |
| 9–14 | Tables, chips, tabs, modals, notices, empty states | shared primitives |
| 15–17 | Dashboard, POS, login | the three screens with their own layout logic |
| 18–20 | Print, mobile chrome, no-backdrop-filter fallback | |
| 21 | Responsive system | the single breakpoint ladder for the whole product |
| 22 | POS header + scan row | |
| 23–26 | Classless controls, mobile tiles, top-bar actions, small-screen parity | |
| 27–29 | Report filter bar, active-tab specificity, report catalogue on tablet | |
| 30–33 | Chip wrapping, `.tabRail`, filter forms, barcode label previews | |
| 34–36 | Admin centre, page-level filter bars, form labels | |
| 37–39 | Narrow-screen rails, `.inventorySearch`, `.productToolbar` grid | |

### Design decisions worth knowing

- **The ambient moved to `<body>`** and became a four-point aurora (ember, indigo, teal, violet)
  instead of a red-only fog. Glass needs something worth refracting; a single hue at high alpha
  just tints the whole screen. Brand red is now used for accents and actions only.
- **Nested glass de-escalates.** A panel inside a panel keeps the tint but loses the blur, the
  shadow and the lit edge (`:is(panels) :is(panels)`), and a third level goes transparent.
  Without this the procurement document builder was three deep and turned milky.
- **The sheen is baked into the `--glass` gradient**, not painted with a pseudo-element. The
  earlier pseudo-element version sat on top of table headers and cell content.
- **One breakpoint ladder** (479 / 767 / 1023 / 1279) replaces the ad-hoc mix of 550, 560, 600,
  620, 650, 700, 760, 800, 850, 900, 960, 1000, 1050, 1080, 1150, 1180 and 1200 px cut-offs.
  Spacing between the stops is fluid (`clamp`), so no width looks cramped or gutterless.
- **Nothing is hidden just because the screen is small.** The previous pass hid `Export CSV` and
  other secondary actions below tablet width; they are back as smaller controls.

### Bugs found and fixed during this pass

- `.topbar .new { font-size:0 }` + `::first-letter` — a mobile "collapse to the first character"
  trick that renders as an **empty red blob**, because `::first-letter` does not apply to an
  inline-flex box. Every page's primary top-bar action was unlabelled on a phone.
- `.posHeader` carried `flex:0 0 64px` inside the terminal's flex column, so once the header
  wrapped, its second row **spilled out of its own box** onto the sale-actions bar.
- `.posHeader` was `position:sticky; top:0` while `.posSaleActions` carried `order:-2`. On desktop
  the terminal never scrolls so it never showed; on a phone the header pinned itself over the bar.
- Tone classes (`.success` / `.warning` / `.error`) are used both as inline chips **and** as
  modifiers on whole cards. Styling them as pills turned the `/expiry` stat cards into ellipses.
  They are now scoped to inline tags, and cards get an accent bar instead.
- `td.emptyTable` (a `colSpan` cell) was given `display:grid`, which collapsed it to one column.
- `.policyGrid` and `.coaGrid` are row lists, not card grids; auto-fitting them into columns
  squeezed each row below its own minimum and pushed controls off-screen on `/cashier-sessions`.
- Several tab rails are built from `<Link>` (an `<a>`), not `<button>`, so the active pill never
  showed on `/procurement`. All segmented-control rules are now tag-agnostic.
- 153 `<button>` elements carry no class at all and were falling back to the user agent's light
  grey chrome — white buttons on dark panels.
- `.reportFilters` is a legacy `display:flex; flex-wrap:wrap` row. Once the filter grid became
  auto-fit its flex base shrank enough to sit beside the preset rail, and both stretched to the
  container height — the four period presets rendered as full-height lozenges.
- `.salesFilters` declared eight fixed-minimum columns (1086px of hard minimum) and pushed its
  Reset button off-screen at 1280px.
- `.barcodeVisual` is a printed label preview (white paper, black ink) and had been swept into the
  glass surface list, so every barcode was black-on-black and unreadable.
- `.tabRail` on `/operations` is a vertical list of icon-plus-label rows; the segmented-control
  treatment stacked each item into a circle.
- `.adminSummary` and `.catalogStats` are inline counters, not metric grids — auto-fitting them
  stacked "1 Unread / 0 Critical / 0 Archived" into a thin column beside the page heading.
- `.productToolbar` is a full-width search row, not a top-bar action cluster; right-aligning it
  squeezed the promotions search field to a stub.
- `.coaGrid` at a 340px minimum gave the chart of accounts three columns, which forced every row's
  two action buttons onto separate lines and doubled its height.
- Most forms are written as `<label>Caption<input/></label>` with no wrapper, so captions sat on
  the same line as their control ("Payment accountSelect account").
- `.reportCatalogue` computes to `flex-direction:column`, so the tablet chip cloud needed an
  explicit `row` to stop every report name becoming a full-width bar.
- A `flex-direction:column` container that still wraps spills items into a second **column** once
  its height is constrained — which is how the promotions status select ended up rendered 10px to
  the right of its toolbar at 390px.
- Both children of `.productToolbar` carry `width:100%` from the original stylesheet, so as a flex
  row each claimed a whole line and the status select always sat below the search box. It is an
  explicit two-track grid now.
- `.inventorySearch` is a container (labelled input plus a record counter), not a search field;
  styling it as a field squeezed the input to a stub beside the count.

### POS terminal changes (requested)

- Removed the duplicate nav (`Home | Sell | Orders | Customers | Products | Inventory | Returns`)
  — every one of those is already in the sidebar, and it was the main cause of header overlap.
- Removed the clock under the `PerfectOne POS` wordmark (the fixed status chip already shows it).
  The `now` state and its 1-second interval were removed with it.
- The customer picker moved out of the cart panel to sit **left of a shorter scan field**.
- The header reserves the fixed status chip's measured footprint (340 px) instead of guessing.

---

## Verification

Measured with headless Chrome over CDP, every route at 360 / 390 / 430 / 768 / 1024 / 1280 /
1440 / 1920 px: document overflow, any element clipped outside a scroll container, and real
bounding-box overlap against the fixed chrome. Scripts live in the session scratchpad
(`audit.mjs`, `shot.mjs`); re-create them if you need to re-run this.

**Result: 0 horizontal overflow on every route at every width.**

The one remaining report is `/pos` — `globalStatusBar over posBrandBlock` below 900 px. That is a
padding-box false positive: `.posBrandBlock` reserves the chip's width as `padding-right`, so its
border box intersects the chip while its content does not. Confirmed visually at 390, 430 and
768 px.

## Routes

Legend: R = Reviewed, D = Redesigned against the design system, QA = measured desktop + tablet +
mobile with no overflow and no overlap.

### Shell (applies to every route)
- [x] Sidebar — floating glass rail, drawer below 900 px
- [x] Topbar — glass, wraps to a single row with a labelled action on mobile
- [x] Fixed status chip — footprint reserved by every layout that sits under it
- [x] Design tokens — one palette, one material, one breakpoint ladder

### Auth
- [x] `/login` — R D QA (single centred card below 1023 px)

### Workspace
- [x] `/` (dashboard) — R D QA (2-up tiles on a phone, values no longer clipped)

### Sales & Purchasing
- [x] `/pos` — R D QA (header rebuilt, nav and clock removed, customer moved beside the scanner)
- [x] `/sales` — R D QA
- [x] `/promotions` — R D QA
- [x] `/procurement` — R D QA (active tab pill fixed)
- [x] `/procurement/orders` — R D QA
- [x] `/procurement/orders/new` — R D QA (nested glass de-escalated)
- [x] `/procurement/grns` — R D QA
- [x] `/procurement/grns/new` — R D QA
- [x] `/procurement/returns` — R D QA
- [x] `/procurement/returns/new` — R D QA

### Inventory
- [x] `/products` — R D QA
- [x] `/products/[id]` (edit) — R D QA
- [x] `/products/new` — R D QA
- [x] `/catalog-settings` — R D QA
- [x] `/catalog-settings/new` — R D QA
- [x] `/barcodes` — R D QA
- [x] `/inventory` — R D QA
- [x] `/inventory/new` — R D QA
- [ ] `/inventory/transfers/[id]` — styled by the shared primitives, but still no seeded transfer
      id to measure against. Create a transfer and re-run the audit with that id.
- [x] `/expiry` — R D QA (stat cards no longer render as ellipses)
- [x] `/operations` — R D QA

### Business partners
- [x] `/customers` — R D QA
- [x] `/customers/new` — R D QA
- [x] `/suppliers` — R D QA
- [x] `/suppliers/new` — R D QA

### Finance & insights
- [x] `/finance` — R D QA
- [x] `/finance/new` — R D QA
- [x] `/accounting` — R D QA
- [x] `/reports` — R D QA (the report catalogue rail becomes a horizontal scroller on tablet)

### Administration
- [x] `/users` — R D QA
- [x] `/users/new` — R D QA
- [x] `/roles` — R D QA
- [x] `/roles/new` — R D QA
- [x] `/branches` — R D QA
- [x] `/approvals` — R D QA
- [x] `/settings` — R D QA
- [x] `/admin-centre` — R D QA
- [x] `/cashier-sessions` — R D QA (discount-policy rows no longer push controls off-screen)
- [x] `/data-tools` — R D QA
- [x] `/backups` — R D QA
- [x] `/sync` — R D QA

## Next up

1. `/inventory/transfers/[id]` — create a real transfer, then screenshot and measure it.
2. Light theme. The app forces dark at boot (`layout.tsx`), so the light palette in section 1 is
   written and consistent but has never been rendered. If light mode is ever switched on, audit
   it the same way before shipping.
3. Backend work is unchanged by this pass; nothing in `services/api` was touched.

## Backend bugs found and fixed in pass 1 (kept for history)

- [x] Promotions: Go date-scan crash (`promotion_management.go`)
- [x] Migrations 043–062 never applied (20 files, incl. `business_registration_no`)
- [x] `/admin-centre` notifications query referenced non-existent `b.created_at` (`notifications.go`)
