# Daily cash projections

Books calculates an end-of-day balance for every included bank account, for every
calendar day from an opening snapshot through the selected horizon (up to two
years). Each daily row has opening balance, inflows, outflows, closing balance,
required floor, shortfall and the individual movements. Separate totals show
bank cash, working cash excluding reserved accounts, card balances and money in
transit. The lowest daily balance is reported per account.

## Run a scenario

Use a registered company and a `books.cash-plan/v1` JSON file:

```sh
books --company example --json cash-forecast --input cash-plan.json
books --company example --format csv cash-forecast --input cash-plan.json
```

The HTTP equivalent is `POST /v1/companies/{company}/operations/cash_forecast`;
MCP exposes `books_company_cash_forecast`. These require company read permission,
perform the same validation, and never post journals or move money. The input
contains `version`, `name`, `currency`, `as_of`, `through`, `accounts`, `events`
and `assumptions`. The [API schema](schemas/books-api-v13.openapi.json) defines
all fields. Amounts are canonical signed integer **minor-unit strings**; use the
selected company's currency scale. Card liability balances are negative.

An account specifies its actual Books account `code`, display `name`, `kind`
(`bank` or `card`), `opening`, nonnegative `floor`, `reserved` flag and `evidence`.
Only active compatible company accounts are accepted. Opening balances are
explicit evidence-backed **end-of-day snapshots on `as_of`**, not an assertion
that the backend retrieved current bank balances. Include every owned bank
account relevant to the scenario; leave excluded owners out. Keep reserved cash
visible but separate from working cash. Account purposes are independent from
an expense's economic category.

Events have a unique `id`, `date`, `name`, `kind` (`inflow`, `outflow`, `transfer`),
`account`, positive `amount`, `status` (`confirmed`, `estimated`, `proposed`,
`actual`), `evidence` and optional `category`. Transfers also require
`to_account` and `arrival`. A transfer debits the source on departure and credits
the destination on arrival. A bank-to-card transfer is the payment of a liability;
card purchases are outflows on the card, not simultaneous bank expenses.

Each recurring occurrence is an explicit dated event. The input producer must
expand salary schedules, bill recurrences, card statement cycles and spending
assumptions into those dates, retaining their basis. This version does not infer
recurrence, predict issuer statement cycles, fetch bank balances or automatically
propose transfers. Separate scenario files can represent no transfers, proposed
funding, changed pay or changed spending. A monthly reserve contribution is a
transfer, not a fabricated monthly vendor debit.

## Actuals, rolling forward and comparison

An actual event may name an expected event in `replaces`. Both remain in the
plan, but the expected movement is suppressed. The result exposes their dates,
amounts and variance. Matching is explicit, same account/kind/destination and
one-to-one; merchant/amount coincidence never auto-matches a payment. The
producer must verify actual-event evidence against the ledger. Updating the
forecast does not alter actual accounting.

An unmatched non-actual event dated on or before the opening snapshot is rejected.
Resolve it using actual evidence or explicitly move its expected date forward;
an unpaid bill must not silently vanish when refreshing a forecast. Actual
transfers departing before the snapshot and arriving later retain in-transit
cash. An actual that fulfilled an expectation before the opening snapshot
suppresses that expectation without debiting the opening cash again.

The result includes the complete input and its SHA-256 digest. Retain versioned
plan/result files (or the existing authorized artifact store) for reproducibility
and forecast-versus-actual comparisons. These files are sensitive financial data;
store them outside the source checkout. This version requires no ledger schema
migration and does not add a planning database or background scheduler.

## Web interface

Set `BOOKS_FORECAST_PLANS_FILE` on the web server to an operator-owned JSON file:

```json
{"example":{"baseline":"/private/example-baseline.json","funded":"/private/example-funded.json"}}
```

The selected company overview shows scenario/account selectors, a daily step
chart, daily balance table, movement and transfer details, assumptions, funding
gaps and actual-versus-expected differences. The web server reads only the
configured file paths and invokes the authenticated company forecast API; it
never exposes API credentials or accepts file paths from a browser. The UI does
not edit plans or execute transfers. Updating a configured plan is reflected on
reload. CLI/API/MCP can calculate any supplied scenario independently.

## Interpretation

Confirmed is evidence supplied by the planner, not a bank guarantee. Display
estimated dates and amounts as estimates. Future-dated actual events represent
known activity relative to an older opening snapshot, not future certainty.
Results are end-of-day positions; same-day movement order is deterministic
(debits first), but intraday bank availability and overdraft timing are not
established. Unmodeled bills cannot be discovered by arithmetic. Currency
conversion and multiple currencies within a company are unsupported.
