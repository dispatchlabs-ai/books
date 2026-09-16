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
