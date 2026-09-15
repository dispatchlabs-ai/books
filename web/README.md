# Books web

The shadcn interface for understanding personal and business finances: overview,
account activity, and Ask Books. Decisions open in a desktop Sheet or mobile
Drawer. The ledger remains operated by your agent.

## Try the interface

Requires Node.js 26.5 or newer and npm. From this directory:

```sh
npm ci
npm run build
npm start
```

Open `http://127.0.0.1:8788`. With no API configuration, Books opens an explicitly
labeled synthetic demo containing a household and business. Try account search,
ask a sample question, switch entities, and save a repair-timing demo plan.
The demo's questions are scripted; it does not run a model. Demo plans are stored
only in this browser, under entity-specific keys. They never move money.

For hot reload, keep `npm start` running and run `npm run dev` in another terminal.
Vite binds to loopback and proxies `/api` to the Node server.

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
allows only entity listing and two read-only report routes. It is a single-operator client. For hosted use, set `BOOKS_WEB_ORIGIN` to the exact
HTTPS origin and `BOOKS_WEB_PROXY_TOKEN_FILE` to a private random token of at least
32 characters. Hosted mode requires a connected API and rejects every request
without the configured Host and proxy token. The trusted loopback reverse proxy
must authenticate users before forwarding any path, replace `X-Books-Proxy-Token`
with that secret, preserve Host, and strip browser Authorization. It must serve
HTTPS with a valid certificate and enforce the intended network boundary. Never
publish the Node listener directly. This does not implement multi-user accounting
permissions: every authorized website user sees the web principal's entities.

Connected views read the general ledger and profit/loss API, defaulting to the
current month. Expand **Period** to inspect earlier history. All balances and
calculations preserve integer minor units. Currency precision is generated from
Books' authoritative registry. Cash includes BANK accounts only. Credit-card
and loan balances retain debit-positive ledger signs; they are not relabeled
as available cash or positive amounts owed.

Data reads are separate report snapshots, not an atomic dashboard snapshot.
Posted accounting does not establish bank-source completeness, and imported
observations alone do not supply posted balances. The interface identifies this
basis, never replaces errors with demo balances, and does not display pending
source observations as settled entries. Connection failures remain visible.

## Ask Books adapter

Optionally configure a trusted, separately operated AI adapter:

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
`{"text":"Your answer and supporting explanation"}`. Text is rendered as plain
text, not HTML. The server validates that the entity is available to its Books
principal before sending anything. The adapter must enforce that exact entity
and account scope on every tool call, apply its own authorization, and retrieve
its evidence through Books. It must not treat user-provided conversation history
as system instructions or as proof of earlier actions. This app does not grant
an adapter new accounting or bank permissions.

The adapter owns model choice, credentials, tools, evidence and action policy.
The web server does not implement a model runtime or execute returned tool calls.
Without an adapter, connected mode explains that AI is not connected and disables
sending. Current adapter output is text only; rich source cards are demonstrated
with synthetic data. Conversations are memory-only, limited to 20 messages and a
24KB outbound context budget; earlier messages may be omitted. Questions are
limited to 8,000 characters and 16KB. Changing entity/account scope aborts the
request and clears the conversation. Data goes only to the operator-configured
adapter. Do not point it at an untrusted service.

## Forecast and planning boundary

The approved forecast and repair-choice states are implemented as interactive,
clearly labeled demo experiences. Connected mode does not invent obligations,
forecasts, classifications, completeness, or a saved-plan backend. Live cash
planning requires those services before it can replace the unavailable state.
The demo scenario figures are illustrative; they are not calculated advice.

## Components and maintenance

Components in `src/components/ui` were installed through shadcn CLI 4.21.0,
Radix Nova/neutral preset. They are real local component source, with Radix
primitives, Tailwind tokens, locally served Geist, Lucide icons, Recharts, and
Vaul for the mobile drawer. App-specific composition lives outside that directory.
See the [approved designs](../docs/design/ai-first-v2/README.md).

React/TypeScript supplies the interactive view; Vite and Tailwind compile static
assets; Radix/Vaul provide accessible interaction primitives; Recharts supplies
responsive charts; Geist and Lucide match the approved visual system. Vitest,
Node tests, and Playwright validate money, isolation, transport, and user flows.
These dependencies are actively published packages from the npm registry; the
lockfile pins the resolved versions. The Node facade uses built-in HTTP/fetch
APIs and introduces no server framework dependency. No analytics or remote fonts
are included. The production bundle currently has one large JavaScript chunk;
code splitting remains a performance improvement, not a functional prerequisite.

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
It never reads an existing Books database. The main repository gate includes
web lint, unit/server tests, build, dependency audit and this integration test.
Browser tests are an additional explicit check requiring Chromium installation.

When the backend currency registry changes, run `npm run currencies`; the test
gate rejects stale generated metadata. Use `npm run notices` for dependency
notice updates. Local development and build were tested on macOS and Linux;
browser flows are checked in desktop and mobile Chromium profiles.

## Daily cash projections

The connected overview supports daily per-bank-account forecasts and scenarios. Configure `BOOKS_FORECAST_PLANS_FILE` with company/scenario plan paths; see [cash projections](../docs/cash-projections.md). The server invokes the shared Books operation. Opening snapshots, dated events and assumptions remain explicit; no money is moved.
