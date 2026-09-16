# Books web

The browser interface is a single Cash screen for personal and business
entities. It shows current posted bank balances, monthly spending averages and
budgets, and shortfalls from configured daily forecasts. The ledger remains
operated through Books' CLI, HTTP API or MCP.

## Try the interface

Requires Node.js 26.5 or newer and npm. From this directory:

```sh
npm ci
npm run build
npm start
```

Open `http://127.0.0.1:8788`. With no API configuration, Books opens an
explicitly labeled synthetic demo containing a household and business. Switch
entities to inspect their bank balances. To preview actual forecast calculations
against invented records, use the [forecast preview](#daily-cash-projections).

For hot reload, keep `npm start` running and run `npm run dev` in another
terminal. Vite binds to loopback and proxies `/api` to the Node server.

## Connect Books

Start a separately configured [Books HTTP server](../docs/api.md#run-locally).
Give the web principal only `read` permission for the intended entities. Store
its random bearer token in a private file outside this checkout, then set:

```sh
export BOOKS_API_URL='http://127.0.0.1:8484'
export BOOKS_API_TOKEN_FILE='/absolute/private/path/client-token'
npm start
```

These variables are read only by the Node server; never use `VITE_` variables
for credentials. The browser receives neither the token nor upstream URLs.
Remote API connections require HTTPS. The web server itself listens only on
loopback, validates Host/Origin, uses a same-origin CSP, disables caching, and
allows only entity listing, two read-only report routes and configured cash
forecasts. It is a single-operator client. For hosted use, set
`BOOKS_WEB_ORIGIN` to the exact HTTPS origin and `BOOKS_WEB_PROXY_TOKEN_FILE` to
a private random token of at least 32 characters. Hosted mode requires a
connected API and rejects every request without the configured Host and proxy
token. The trusted loopback reverse proxy must replace `X-Books-Proxy-Token`
with that secret, preserve Host, and strip browser Authorization. It must serve
HTTPS with a valid certificate and enforce the intended network boundary. Never
publish the Node listener directly. If no application login is configured, the
proxy must authenticate every path before forwarding. This does not implement
multi-user accounting permissions: every authorized website user sees the web
principal's entities.

Connected views read the general ledger and profit/loss API, defaulting to the
current month through today. All balances and calculations preserve integer
minor units. Currency precision is generated from Books' authoritative registry.
Cash includes BANK accounts only. Credit-card and loan balances retain
debit-positive ledger signs; they are not relabeled as available cash or
positive amounts owed.

Data reads are separate report snapshots, not an atomic dashboard snapshot.
Posted accounting does not establish bank-source completeness, and imported
observations alone do not supply posted balances. The interface identifies this
basis, never replaces errors with demo balances, and does not display pending
source observations as settled entries. Connection failures remain visible.

## Browser scope

Cash is the only application screen. The September 16 design supersedes the
experimental Accounts, Outlook, account-detail, Ask Books and decision-demo
screens. Their earlier design records remain in Git. Removing those screens does
not change the accounting engine, forecast calculations, or server API. The
optional `/api/ask` adapter remains a server interface; Cash does not use it.

Week / Month / Year selects a rolling forecast horizon of 7 / 30 / 365 days.
Rows remain sorted by their first projected negative date, then name. Reserve
accounts stay visible; card liabilities are excluded. Incomplete horizons and
unplanned banks cannot establish that an account stays nonnegative.

The monthly comparison is the average of the previous two complete months'
posted expenses by budget bucket. It stays monthly across forecast horizons.
Unset targets, missing data, unassigned spending, errors and retry controls
remain distinct. Editing budgets uses the existing revisioned, separately
permissioned budget operation. It never rewrites dated forecasts.
Click a target amount or **Not set** to edit in the Cash table with Save and Cancel
controls; spending assignments expand below it. The last accessible entity is remembered across
reloads when browser storage is available, separately for demo and connected use.

## Components and maintenance

Components in `src/components/ui` were installed through shadcn CLI 4.21.0,
Radix Nova/neutral preset. They are real local component source, with Radix
primitives, Tailwind tokens, locally served Geist and Lucide icons. The wider
installed library also retains chart and drawer components, unused by Cash.
App-specific composition lives outside that directory. See the
[Cash design](../docs/design/cash/README.md). Older design records are
historical, not approval of additional screens.

React/TypeScript supplies the interactive view; Vite and Tailwind compile static
assets; Radix provides accessible tabs, menus, selects and disclosures;
Geist and Lucide keep typography and icons consistent. Cash trends use
exact-integer inputs scaled into SVG coordinates. Vitest, Node tests, and
Playwright validate money, isolation, transport, and user flows. These
dependencies are actively published packages from the npm registry; the lockfile
pins the resolved versions. The Node facade uses built-in HTTP/fetch APIs and
introduces no server framework dependency. No analytics or remote fonts are
included. The browser bundles only Cash and its supporting components.

Licenses and original notices are retained in
[the distributed notices](public/THIRD_PARTY_NOTICES.txt). Update them with
`npm run notices` after dependency changes and before redistributing assets.
`npm audit` checks known advisories; it is not a comprehensive security audit.

## Checks

```sh
npm run lint
npm test
npm run build
npx playwright install chromium
npm run test:e2e
npm run test:integration
```

The integration test requires the repository's Go/CGO prerequisites and builds
Books against a fresh temporary home, posts invented transactions, reads them
through the actual HTTP API and web facade, checks scope, and runs Doctor/audit.
It never reads an existing Books database. The main repository gate includes web
lint, unit/server tests, build, dependency audit and this integration test.
Browser tests are an additional explicit check requiring Chromium installation.

When the backend currency registry changes, run `npm run currencies`; the test
gate rejects stale generated metadata. Use `npm run notices` for dependency
notice updates. Local development and build were tested on macOS and Linux;
browser flows are checked in desktop and mobile Chromium profiles.

## Daily cash projections

Entities with a configured cash plan show per-account trends, first negative
dates and minimum projected end-of-day balances on Cash. Configure
`BOOKS_FORECAST_PLANS_FILE` with company/scenario plan paths; Cash uses the
first configured scenario. See [cash projections](../docs/cash-projections.md).
The server invokes the shared Books operation. The opening snapshot and scenario
remain visible; no money is moved and no plan is changed by the interface.

To try it with invented data, run `npm run build` and then
`npm run preview:forecast`. It builds Books into a new temporary home, creates a
synthetic household and its baseline and funded plans, and serves the connected
interface at `http://127.0.0.1:8790`. Stopping it removes the temporary
directory. It requires the Go prerequisites and never reads an existing Books
database.

## Browser sign-in and feedback

Hosted deployments can set `BOOKS_WEB_LOGIN_FILE` to a private JSON file with
`username`, `salt` (64 lowercase hex characters), and `hash` (128 lowercase hex
characters). Generate it with the exported async
`passwordRecord(username, password)` helper in `session-auth.mjs`, passing
credentials through a private process environment or secret store. It uses Node
scrypt with a random salt; never put passwords in command arguments or
repository files.

With this file configured, Books serves `/login` as HTML and authenticates every
other route before reading data. The proxy still enforces HTTPS, Host and its
private token, but does not need HTTP Basic authentication. Cookies are random,
Secure, HttpOnly, SameSite=Strict, host-only and expire after twelve hours.
Sessions are held in memory: restarting the service or changing the login file
and restarting revokes them. `POST /logout` revokes the current session. Login
attempts are limited to six per minute globally for this single-operator app;
POST requests require the exact Origin. This is not multi-user authorization.

Optional `BOOKS_WEB_FEEDBACK_FILE` is a private JSON configuration containing
absolute `sources`, `snapshot`, and `answers` paths. Sources contain
`records[].source`; the snapshot contains `transactions[].source_uid` including
resolved records. Existing answers are retained and saved atomically; notes do
not post ledger entries. Session login is mandatory for feedback. Its
`/feedback/` page and JSON routes run inside the same supervised server. An
optional `page` path serves a trusted operator-owned HTML template, preserving
prior annotations; only the exact inline scripts in that file receive CSP
hashes. Keep this file operator-controlled, never supplied by an HTTP request.
The bundled page is a minimal USD/minor-unit review interface; use an
appropriate operator template for other transaction display conventions.

## Retained AI adapter endpoint

Cash does not call this endpoint. Existing integrations may continue to use the
optional server adapter by configuring a trusted, separately operated service:

```sh
export BOOKS_AGENT_URL='https://agent.example/ask'
export BOOKS_AGENT_TOKEN_FILE='/absolute/private/path/agent-token'
npm start
```

The server sends a JSON POST:

```json
{
  "company": "example",
  "account": "1000",
  "messages": [{ "role": "user", "content": "Why did spending rise?" }]
}
```

`account` is omitted for an entity-wide conversation. The response is
`{"text":"Your answer and supporting explanation"}`. The server validates that
the entity is available to its Books principal before sending anything. The
adapter must enforce that exact entity and account scope on every tool call,
apply its own authorization, and retrieve its evidence through Books. It must
not treat user-provided conversation history as system instructions or as proof
of earlier actions. This app does not grant an adapter new accounting or bank
permissions.

The adapter owns model choice, credentials, tools, evidence and action policy.
The web server does not implement a model runtime or execute returned tool
calls. Without an adapter, `/api/ask` reports that AI is unavailable. Questions
and conversation context remain bounded by the facade's validation. Clients are
responsible for clearing conversations when company or account scope changes.
The removed browser conversation UI is not part of the current Cash experience.
Do not point the configured adapter at an untrusted service.
