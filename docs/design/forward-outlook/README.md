# Outlook: will income cover spending?

A connected entity with a saved cash plan gets an **Outlook** destination beside
Overview and Ask Books. It answers one question before showing detail:
does expected income cover planned spending, month by month, from here forward?

The screen is point-forward only. Posted history, the ledger period picker and
profit and loss stay on Overview. The Overview shows a compact *Looking ahead*
card that links here; it never blends forecast figures into recorded totals.

## What a person should understand

In order of the page:

1. **The answer.** One sentence, the average left over (or short) per full
   month, the tightest month, and each full month's income, spending and left
   over with a bar showing spending as a share of income.
2. **Reserves.** How net reserve balance changes and transfer timing affect
   other bank and card balances, per month and per reserve account. A prominent
   notice in both scenarios explains that full reserve goals cannot be assessed
   without structured targets, contribution schedules and cap rules. Scenario
   funding must never be described as full-goal affordability.
3. **Account gaps.** Accounts that fall below their floor, from which date, for
   how many days and how far. A balanced household can still run one account
   short on the wrong day.
4. **Daily balances.** The existing per-account daily drilldown: chart, floor,
   lowest point, days, movements and evidence.
5. **How this is calculated.** Definitions, plan coverage, event statuses,
   assumptions and the plan digest.

Freshness is always visible: "Estimates from balances on Sep 14 (yesterday) ·
Not updated automatically". Months that have already ended are marked
**Past · estimate** so an old plan can't pass for actuals.

## Semantics

Implemented once in [`web/src/lib/forecast.ts`](../../../web/src/lib/forecast.ts)
with integer minor units. Unit tests cover each rule; the web integration test
runs the real Books engine on the synthetic household and checks the figures
and the balance identities below.

| Term | Definition |
| --- | --- |
| Horizon | Movement legs after `as_of` through `through`, including arrivals from opening transit and excluding expectations replaced by an actual. |
| Income | Every bank-account `inflow`. |
| Spending | Every `outflow`, from bank accounts **and cards**, less card refunds/credits. Card purchases count once; bank-to-card payments and all transfers are never spending. Outflows to accounts outside the plan count here. |
| Reserve balance change | Net change in reserved bank accounts: deposits on arrival, less withdrawals on departure and reserve-paid expenses. Includes each leg between reserves and retains zero-net activity. |
| Change in other balances | Left over less reserve balance change and net increase in transit. Equals the monthly change in non-reserve bank and card balances; this is not a contribution total or full-goal affordability verdict. |
| Month | Calendar month of each movement; transfers use departure for the source and arrival for the destination. Opening transit and arrivals beyond the horizon are respected. A month is **full** only if the plan covers its first and last day. |
| Averages | Full months with income/spending activity that have not ended, rounded half away from zero to a minor unit. Partial, ended and unplanned months remain visible with their exclusion reason. Income, spending and surplus averages are independently rounded from exact totals. |
| Account gap | A bank account with shortfall on any day from the opening snapshot: first date, days short, lowest balance and shortfall at the lowest point. |

Accounts left out of a plan (for example excluded owners, trusts or
investments) are not operating cash here. Nothing on this screen edits plans,
posts entries or moves money.

A scenario with no proposed events and account gaps shows a **Compare** prompt
toward the other scenarios, since a baseline without funding transfers usually
leaves funded accounts empty.

## Component map

| Area | Components |
| --- | --- |
| Navigation | Existing sidebar `Button` and mobile bottom bar, driven by one page list; Outlook appears only when `/api/config` lists scenarios for the entity |
| Scenario | shadcn `ToggleGroup` (single, outline) |
| Answer, reserves, gaps, summary | `Card`, `Badge`, `Button`, `Collapsible`, `Skeleton` |
| Money | `Amount` ([amount.tsx](../../../web/src/components/amount.tsx)): exact formatting, tabular figures, color only for meaning |
| Month row | `MonthRow` in [outlook.tsx](../../../web/src/components/outlook.tsx), reused for partial months |
| Daily balances | [daily-balances.tsx](../../../web/src/components/daily-balances.tsx): shadcn `Select`, `ToggleGroup` day filter, `Table`, Recharts through `ChartContainer` |
| Data | `useForecast` loads one scenario, caches results, keeps the last good result while switching |

`select`, `table`, `toggle` and `toggle-group` were added with shadcn CLI 4.21.0
(Radix Nova, neutral), matching the existing components.

## Tokens and patterns

- `--positive` / `text-positive`: money left over. `--floor` / `border-floor`:
  the dashed floor line. `text-destructive`: shortfalls and months that are short.
  Light and dark values live in `web/src/index.css`.
- `.stat-label` for small muted labels; `.amount` for tabular, unbroken money.
- The hero number stays foreground colored, like the Overview's cash figure.
  Status color is on the icon and the per-month values.
- Layouts inside cards use **container queries** (`@container`, `@xl:`, `@2xl:`,
  `@3xl:`) because card width depends on the sidebar, not the viewport.
- The *Tightest* badge marks the smallest full month without inventing a
  warning threshold.
- 44px touch targets for scenario, gap, day and disclosure controls.

## States

Loading skeletons; scenario load failure with **Try again** and **Back to** the
first scenario; switching scenarios keeps the prior result dimmed; no full month
yet; a short month; average short; no reserve activity; no gaps; many gaps;
days filtered to *Below floor*, *With activity* or *All days* with a disabled
empty filter; long lists paged at 31 days; entities without a plan show no
Outlook and Overview says no plan is connected. Account detail's Outlook tab
links to that account's daily balances when the plan includes it.

## Design concepts

Generated with imagegen (through Codex) from the approved
[v2 overview](../ai-first-v2/01-overview.png) as the style reference. Names and
amounts are synthetic; they predate final tuning of the synthetic fixture, so
some figures differ from the implementation.

![Outlook on desktop and mobile](01-outlook.png)

![Daily balances for an account gap](02-daily-balances.png)

Prompts: [Outlook](prompts/01-outlook.txt), [daily balances](prompts/02-daily-balances.txt).

Deliberate differences from the concepts: the hero number is not green; the
amber "thin month" treatment was replaced by a neutral
*Tightest* badge; bars have exact spent-share labels, with net card credits
identified instead of a negative percentage; daily balances stay open with a day filter; gap
rows also state the first date and days short; and month rows expand to
spending by category.

## Coverage record

Checked in Chromium on the synthetic household (`npm run preview:forecast`,
real Books engine and web facade).

| Journey or state | Evidence |
| --- | --- |
| Overview summary → Outlook | Playwright desktop and mobile; screenshots at 1440 and 390 |
| Answer, tightest, partial month, category expansion | Playwright; real-engine integration figures |
| Scenario switch, reserves per month and account | Playwright; integration identities per month and scenario |
| Gap → daily balances, focus, filter, evidence | Playwright; keyboard focus lands on the section heading |
| Account change resets filter; empty filter disabled | Playwright |
| Leave and return keeps scenario and account | Playwright |
| Scenario failure, retry, back to first scenario | Playwright |
| Account detail → Outlook for that account | Playwright |
| Demo mode has no Outlook | Playwright |
| No page overflow or clipped text, 320–2560 px in 16/80 px steps, while resizing | Scripted sweep on Outlook (both scenarios, expanded, all days), Overview and account detail |
| Real household plan figures | A private plan was evaluated with this module outside the repository; results are not committed |

Not checked: dark theme (the app has no theme switch yet), screen readers
beyond Chromium's accessibility tree, Safari/WebKit, and live refresh behavior,
which does not exist.

## Continuation review and verification

The saved implementation and all follow-up refinements were retained. The first
independent review found empty months counted as covered, ended months in the
answer, mismatched scenario labels while loading, card credits shown as income,
missing card-plan links and partial-month reserves, and several mobile and
accessibility defects. The refinements address these through the existing
components. A fresh independent review confirmed the resulting implementation with no unresolved material findings.

Additional regression coverage verifies each account's reserve change and the
identity `surplus = reserve change + transit change + other balance change`
against the real engine. Synthetic cases include opening transit, month-end
reserve deposits and withdrawals, transfers between reserves, card payoffs,
and arrivals beyond the horizon. Zero-net reserve activity and excess card
credits have focused tests.

Full-target affordability remains a data limitation: `books.cash-plan/v1`
contains explicit movements and assumptions, not structured funding targets or
cap policies. No personal goals are embedded in the UI. This limitation appears
before reserve figures in both scenarios and next to the operating average.

Final evidence and check results are recorded in [verification](verification.md).
