# Outlook verification

The continuation preserves the initial Outlook implementation (`1441dd4`) and
its saved refinements. It completes the reserve semantics, review corrections
and verification described in the [design reference](README.md).

## Checks

The final implementation passed `./scripts/check` on Linux and macOS using
fresh temporary Books homes with invented records. This includes Go tests and
race tests, vet, lint, vulnerability checks, the real CLI smoke test, web lint,
18 unit tests, seven server tests, production build, dependency audit and the
real Books API/web integration. No known vulnerabilities were reported. The
optional private deployment-name denylist was not configured; synthetic privacy
scanner checks passed. The existing large-bundle warning remains.

Chromium Playwright passed all 14 desktop/mobile flows after the final build.
Both scenarios were inspected at 320, 390, 768, 1024, 1440 and 2560px. Resizing
sweeps between 320 and 2560px found no page overflow or clipped text. Screenshots
below contain only the invented Maple Household.

| Journey / transition | Result |
| --- | --- |
| Overview → Outlook; return to Overview | Working; scenario retained, forecast separate from posted accounting |
| Monthly answer; spending detail; partial/ended/unplanned months | Working; exclusions visible and omitted from averages |
| Scenario switching and slow requests | Prior data retains its actual scenario label while loading |
| Scenario error → retry / back to baseline | Working; error never becomes demo data |
| Account gap → daily balances | Focus moves to section heading; reduced motion respected |
| Account change; filters; day detail; pagination; evidence | Working; account-specific state resets and empty shortfall filter is disabled |
| Card account → Outlook | Card inclusion accurately explained; purchases/credits handled once |
| Refresh / return | Refresh fetches the saved plan; no automatic bank refresh is implied |
| Leaving with unsaved forecast work / submitting transfers | Not applicable: Outlook is read-only and has no editable plan or payment submission |
| Reserve and monthly balance identities | Real-engine tests cover opening transit, month-end arrivals, withdrawals, between-reserve transfers, card payoffs and arrivals beyond the horizon |

## Independent review

A fresh read-only reviewer inspected the current code, real shared component
usage, design references and browser behavior after the initial review's fixes.
The reviewer independently confirmed the corrected reserve/transit identities,
card credits, prominent full-goal limitation, responsive layouts, focus,
scenario attribution, error/retry, refresh and pagination. No unresolved
material findings remained.

## Representative before and after

| | Desktop | Phone |
| --- | --- | --- |
| Before: posted overview with per-account forecast | [Screenshot](verified/before-desktop.png) | [Screenshot](verified/before-phone.png) |
| After: aggregate Outlook with reserve and funding-gap detail | [Screenshot](verified/after-desktop.png) | [Screenshot](verified/after-phone.png) |

## Limits

Full reserve-goal affordability remains unassessed: the plan has movements and
assumptions but no structured target/schedule/cap data. The UI says this
prominently in every scenario. No personal targets are hardcoded.

Browser verification covers Chromium, including accessibility-tree and keyboard
inspection. Safari/WebKit, a screen-reader session and dark theme were not
checked. Private household data was not used for development or these images.
Live deployment and its exact revision are operational checks separate from
this synthetic acceptance record.
