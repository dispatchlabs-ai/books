# Cash refinement verification — September 16, 2026

The Cash-only browser refinement preserves bank-only forecasts, exact monetary
values, previous-two-month spending averages, revisioned monthly targets and
entity isolation. Backend operations and authentication are unchanged.

- macOS: `./scripts/check` passed, including Go tests/race/vet/lint, vulnerability
  checks, web lint/unit/server tests, production build, dependency audit and the
  real CLI/API integration using disposable synthetic records.
- Linux: the same gate passed through the production build and dependency audit.
  Its final integration subprocess initially selected an unconfigured Go shim;
  rerunning `npm run test:integration` with the installed Go version explicitly
  selected passed. No machine-wide runtime configuration was changed.
- Browser: all 16 desktop/mobile Chromium tests passed. Flows cover authentication,
  Cash-only navigation, bank exclusions, forecast horizons, missing/error states,
  monthly budgets, entity changes, keyboard tabs, category/purchase assignment
  saves, conflict retention, and 320–1440 pixel overflow checks.
- Independent review found four accessibility/layout issues: label contrast,
  current-entity announcement, narrow demo header overflow and narrow budget
  Sheet overflow. Repairs were re-reviewed with synthetic browser fixtures; no
  actionable findings remained.
- Visual checks inspected the compact table, shortfall color/trends, responsive
  account rows and budget Sheet. Product design acceptance remains pending the
  maintainer's review; passing tests does not establish visual approval.

All development and test records were synthetic. No existing financial database
was accessed during development.

## Review follow-up: inline budgets and remembered entity

The maintainer accepted the Cash layout and requested a replacement for the
budget Sheet plus a fix for entity selection resetting on browser reload.
Targets now edit in the table with Save/Cancel and a collapsed assignment
section. Accessible entity selection persists separately for demo and connected use.

- The full `./scripts/check` passed on both macOS and Linux, including the real
  CLI/API integration with disposable synthetic data.
- All 24 desktop/mobile browser tests passed. Regression coverage includes
  connected entity reloads and removed access, blocked storage, inline save and
  cancellation, zero versus unset, invalid amounts, conflicts, assignment
  preservation, keyboard period changes and widths from 320 to 1440 pixels.
- Independent review reproduced and confirmed repairs for late bank rows being
  omitted from saves, period changes taking focus from tabs, and an older budget
  read replacing a newer save. All three targeted reproductions passed after
  repair, with no remaining actionable findings.
- The connected synthetic preview now permits budget edits in its disposable
  company. The inline inputs were visually inspected in the existing table.

The replacement editor awaits maintainer visual review. Existing accounting,
forecast arithmetic and real budget records were not changed by development.

## Review follow-up: click the budget amount

The maintainer accepted the inline direction and requested direct activation
from a budget number instead of a separate Edit budgets button. Clicking an
amount or Not set now focuses and selects the corresponding input; Save and
Cancel return focus to that amount. Read-only entities retain plain values.

- The full `./scripts/check` passed on macOS and Linux. After the focus repair,
  web lint, unit/server tests and production builds were repeated on both hosts;
  the CLI/API integration had already passed with the unchanged backend.
- All 26 desktop/mobile browser tests passed. Coverage includes amount and
  Not set activation, keyboard selection, Save/Cancel focus, read-only access,
  existing draft/assignment/refresh regressions and a scrolled 25-bank table.
- Independent review reproduced an input hidden by the sticky heading. The
  repair scrolls the focused input below the measured controls and prevents a
  reduced-motion rule from introducing a heading padding transition. The exact
  desktop/mobile reduced-motion reproduction passed after repair, with no
  remaining findings; the regression is included in the browser suite.

Development used only synthetic records. No production targets were changed.

## Review follow-up: keep the screen in place

The maintainer reported a jump when clicking a budget amount. The earlier editor
added heading padding, a helper line and a wrapped action row on phones, then
scrolled to the input. The repair keeps heading geometry and sticky behavior
constant, places Save/Cancel in existing header space, removes the helper line,
and focuses entry/exit without scrolling. Error recovery can still reveal a
field or message.

- Full `./scripts/check` passed on macOS and Linux. The first Linux source
  transfer included macOS metadata sidecars; a clean transfer excluding those
  sidecars passed the complete gate. No repository behavior was changed for
  that transfer issue.
- All 26 desktop/mobile browser tests passed. The regression asserts identical
  viewport scroll and all 25 account-row rectangles before/after entry and
  cancellation, both at the top and farther down the table, with reduced motion.
- Independent review confirmed exact positions at 320, 390, 761 and 1280 pixels,
  including rows near the sticky heading. Save/Cancel do not overlap the title
  or tabs, there is no page overflow, and invalid-input/server-error focus stays
  visible. Desktop and 320-pixel screenshots were visually checked.

All test records were synthetic; no production target was changed.

## Review follow-up: account order and hiding

The maintainer requested drag ordering and optional hidden accounts, with the
arrangement shared across devices. Cash now composes dnd-kit sorting with the
existing shadcn rows and menus. Server storage is display-only and per entity;
local/demo previews retain isolated browser storage.

- Full `./scripts/check` passed on macOS and Linux, including disposable CLI/API
  integration, dependency audit (zero reported vulnerabilities), and server
  tests for persisted views, entity isolation, stale revisions, payload limits,
  corruption and access denial. The final focus repair received repeated Linux
  web lint/unit/server/build/integration checks against the final source.
- All 34 desktop/mobile Chromium tests passed. New coverage includes pointer and
  touch drag, keyboard reorder and cancellation, reload persistence, hide and
  restore (including all-hidden recovery), independent-device conflicts, and
  retention of hidden budgets and assignments. Existing no-jump checks pass.
- Independent review caught an overly restrictive account-code grammar and
  offscreen focus after restoring a distant row. Both were repaired and
  re-reviewed. The reviewer also verified delayed preference reads and pending
  restore saves cannot remove a focused budget editor, and that hiding returns
  focus to an adjacent row. No actionable findings remain.
- Desktop and phone views were visually checked. The dnd-kit addition increases
  the main bundle by approximately 38 KB gzip; the build reports its 500 KB
  uncompressed chunk-size advisory. The full build and checks pass.

macOS checks used the installed Command Line Tools through `DEVELOPER_DIR` after
an unrelated Xcode selection required its license setup. No global toolchain
configuration was changed. Development used synthetic data only. Visual product
acceptance remains the maintainer's review, separate from these checks.

## Expected income

Cash now shows the selected period's expected external bank receipts in a
compact disclosure above the accounts. It reuses the configured scenario and
does not infer recurring pay, rewrite budgets or execute transfers.

- Full repository gates passed on macOS and Linux, including disposable CLI/API
  integration and dependency audit. Development fixtures were synthetic.
- All 42 desktop/mobile browser flows passed, including period totals, dated
  payment details, partial/expired coverage, failure after a successful load,
  exact large amounts on a 320-pixel phone and existing no-jump editing.
- Unit coverage checks exact sums, opening-day and horizon boundaries, actual
  replacements inside/outside the window, reserve receipts and exclusion of
  internal transfers and card credits.
- Independent review found a narrow-screen overflow for large supported amounts.
  Wrapping the summary and stacking receipt amounts on narrow phones repaired
  it. Re-review verified 320/360/390-pixel layouts and keyboard expansion, with
  no remaining findings. Separate checks preserved expanded-income budget-edit
  geometry at 320, 390 and 1280 pixels. Desktop and phone screenshots were read.

Income amounts and payment assumptions are private scenario inputs, not public
fixtures or application defaults. Product acceptance remains the maintainer's
review.
