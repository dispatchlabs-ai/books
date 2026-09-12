# One currency per entity

A Books entity can represent a company or a person. Choose its monetary
currency at initialization; `company` remains the CLI registry term for both.
USD is the default. For example:

```sh
books init --name "Example Personal Books" --company personal --currency JPY --start 2026-01-01
books account add bank Checking
books receive 1250 Revenue "Synthetic receipt" --to Checking --date 2026-01-15 --key receipt-1
books tb --as-of 2026-01-31
```

The supported monetary ISO codes are exposed by `GET /v1/capabilities`.
Currency metadata is maintained in `internal/money/currencies.go`. Nonmonetary
codes without a defined minor unit are rejected. Currency precision follows
that registry: JPY uses zero decimals, USD/EUR two, KWD three, and CLF four.
Amounts remain signed 64-bit integers. Fractional minor units and overflow
fail; additional trailing fractional zeros are harmless. No rounding occurs.

The entity, its books, and every statement account share a currency. Omitted
statement-account currency inherits its book. A source file can retain another
currency for inspection, but mapping and posting require an exact currency
match. Books does not convert currencies or calculate exchange gains/losses.
Consolidation requires the group and every included book to share a currency.
Different entities may use different currencies, including in one database.

Currency is selected when the entity is created. Editing the company registry
cannot change or reinterpret a book's currency: CLI and API binding verify it
against durable entity/book identity. To keep books in another currency, create
a separate entity and supply independently prepared opening entries in that
currency. There is no in-place conversion command.

## Machine amounts and compatibility

Existing `*_cents` field and column names remain for compatibility, but mean
minor units of the owning currency. HTTP JSON returns integer strings: `123`
means JPY 123, EUR 1.23, KWD 0.123, or CLF 0.0123. Use the owning company,
account, journal, or report scope's `currency`; never assume a divisor of 100.
CLI JSON keeps exact decimal strings in these fields and uses the owning
currency's precision. Source statement amounts remain exact decimal strings.

OpenAPI snapshot 1.2.0 documents this contract; earlier artifacts are immutable.
Routes and envelopes remain v1. The database schema does not change and USD
amounts keep their existing interpretation. Reconciliation plans for non-USD
books use `books.reconciliation-plan/v4`; year-close plans use
`books.year-close-plan/v2`. Both retain integer amounts and bind an explicit
`currency` into the digest. Existing USD reconciliation v3 and year-close v1
plans keep their schema and serialized digest representation. Legacy plans
cannot be applied to a non-USD company. Period-close plans contain no monetary
amounts and retain their existing schema.
