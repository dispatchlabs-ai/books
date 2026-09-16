# Cash

The default screen is a bank-account list with compact shadcn Week / Month /
Year Tabs. It replaces the overview as the entry point. As of September 16, Cash
is the only application screen. The unapproved Accounts, Outlook, account-detail
and Ask Books screens have been removed from the browser. Their underlying
accounting and forecast operations remain available through the supported
backend interfaces.

The approved [imagegen concept](concept.png) uses invented data. Its brief: keep
only the account-list panel, rename it Cash, add Week / Month / Year tabs, and
show each bank account's current balance, first negative date, lowest projected
balance, and a compact zero-relative trend. No aggregate hero, shortfall banner,
quarter selector, or always-open detail panel.

## Semantics

- Rolling horizons contain today and the next 6, 29, or 364 calendar days.
- Negative means end-of-day cash below zero, independently of reserve floors.
  Card liabilities are excluded. Intraday overdrafts are not assessed.
- Sort known shortfalls by first date, then by account name. Reserved bank
  accounts stay visible. Rows are informational and do not navigate to
  unapproved detail screens.
- Cash now comes from the posted ledger through today, not a live bank balance.
  The balance request always ends at today.
- Projection amounts remain exact integer minor units. The chart alone scales
  those integers into bounded pixel coordinates.
- Missing days, future-starting plans, and truncated horizons cannot establish
  that an account stays nonnegative. Partial minima are labelled as such.
- Banks absent from the plan remain visible as not included. Entities without
  plans show bank balances and explicitly unknown future shortfalls.
- Opening dates, plan identity, and incomplete coverage remain visible. Viewing
  Cash never refreshes banking data, creates forecasts, or moves funds.

Generation used built-in imagegen; the raster is a reference, not a runtime
asset.

## September 16 refinement

The Cash-only redesign keeps the existing horizon and budget semantics while
replacing the sidebar and per-account cards with a slim entity header and a
compact, aligned table. Desktop rows use a separate trend column; mobile rows
reflow into labelled values. Known negative dates and amounts use restrained
red. Unknown forecasts, unavailable balances and unset budgets remain explicit.

The implementation reuses the installed shadcn Table, Tabs, Button,
DropdownMenu, Input, Select and Collapsible components. Following maintainer
review, monthly targets are edited directly in the Cash table with explicit Save
and Cancel controls. Category and purchase assignments remain under a disclosure
below the table while editing. Drafts survive period changes and failed saves;
entity switching and refresh are disabled until the edit is saved or cancelled.
An empty input clears the target; zero remains an explicit target.

The browser remembers the last selected accessible entity across reloads, with
separate preferences for demo and connected use. If the remembered entity is no
longer accessible, Books selects the first permitted entity. Blocked browser
storage does not prevent switching; it only prevents remembering the selection.

No new application screen, aggregate hero, speculative chart or marketing copy
is introduced. The September 15 concept and initial September 16 Sheet remain
historical evidence. The maintainer accepted the Cash layout and requested this
replacement for the budget editor; its visual acceptance remains pending review.
