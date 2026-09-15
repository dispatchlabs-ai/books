# Cash

The default screen is a bank-account list with compact shadcn Week / Month / Year
Tabs. It replaces the overview as the entry point. Existing accounting and
Outlook detail workflows remain available under Accounts and Outlook.

The approved [imagegen concept](concept.png) uses invented data. Its brief:
keep only the account-list panel, rename it Cash, add Week / Month / Year tabs,
and show each bank account's current balance, first negative date, lowest
projected balance, and a compact zero-relative trend. No aggregate hero,
shortfall banner, quarter selector, or always-open detail panel.

## Semantics

- Rolling horizons contain today and the next 6, 29, or 364 calendar days.
- Negative means end-of-day cash below zero, independently of reserve floors.
  Card liabilities are excluded. Intraday overdrafts are not assessed.
- Sort known shortfalls by first date, then by account name. Reserved bank
  accounts stay visible. Clicking a planned account opens its Outlook detail.
- Cash now comes from the posted ledger through today, not a live bank balance.
  A historical Accounts period resets when returning to Cash.
- Projection amounts remain exact integer minor units. The chart alone scales
  those integers into bounded pixel coordinates.
- Missing days, future-starting plans, and truncated horizons cannot establish
  that an account stays nonnegative. Partial minima are labelled as such.
- Banks absent from the plan remain visible as not included. Entities without
  plans show bank balances and explicitly unknown future shortfalls.
- Opening dates, plan identity, and incomplete coverage remain visible. Viewing
  Cash never refreshes banking data, creates forecasts, or moves funds.

Generation used built-in imagegen; the raster is a reference, not a runtime asset.
