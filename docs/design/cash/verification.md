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
