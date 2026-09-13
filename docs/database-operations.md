# Whole-database operations

The experimental `books.server/v3` configuration adds operator-selected database
handles. Existing v1/v2 configurations retain their grants and reject these new
fields. Each handle maps to an absolute server path and the database UUID; startup
fails if the identity differs. Clients can select handles but cannot supply paths,
add grants or replace server configuration through this interface.

```json
{
  "schema": "books.server/v3",
  "listen": "127.0.0.1:8484",
  "databases": {
    "example": {
      "path": "/absolute/path/example.sqlite",
      "uuid": "00000000-0000-0000-0000-000000000000"
    }
  },
  "principals": [{
    "id": "database-owner",
    "token_sha256": "REPLACE_WITH_SHA256_OF_HIGH_ENTROPY_TOKEN",
    "databases": {"example": ["read", "manage"]}
  }]
}
```

Replace the path, UUID and credential digest with your own values. Read the UUID
from the existing company's registry or database status; the placeholder is not
a usable database identity. Keep configuration mode 0600. Normal TLS, credential
and origin rules apply. No network service starts automatically.

`read` exposes **all books, entities and evidence in that database**. `manage`
also permits all registered database accounting and topology writes, including
posting, closing and reopening. It is broader than company `manage` and requires
`read`. A company's grants never authorize database operations, even if that
company shares the target SQLite file. Grant database access only when whole-file
authority is intended. Backup/restore/registry administration are still pending
and are not silently enabled by these initial bindings.

Invoke `POST /v1/databases/{database}/operations/{operation_id}` with a JSON body.
Each operation has its own typed schema and required grant in the
[OpenAPI snapshot](schemas/books-api-v8.openapi.json). Reads also use POST because
their complete typed selectors belong in the body; their descriptors mark them as
read effects. Unknown fields, duplicate keys and query parameters are rejected.
64-bit integers are exact canonical strings; monetary values are minor units.
Raw evidence JSON preserves its own numbers and structure. Responses use the
existing `books.api/v1` envelope. A successful validation response can contain
`valid: false`; inspect domain results, not just HTTP status.

For example, `entity_list` takes `{}`. `report_trial_balance` accepts
`{"group":"ALL","as_of":"2026-01-31","include_zero":true}` for an existing
consolidation group. The database grant covers the entire consolidation perimeter;
no partial-company result is silently substituted. Existing company report routes
remain unchanged. The CLI retains its current flags and exact decimal output.

The backend descriptor registry explicitly binds operations to ledger/report
services. It is not a shell runner and accepts no SQL. Schema generation uses
`go run ./internal/contractgen`; the generator reads source contracts and a prior
OpenAPI snapshot, never financial data. Historical snapshots remain immutable.

Current bindings include entities/books/groups/ownership, accounts and identities,
period operations, journal drafts/posting/import, source records/links, statement
accounts/imports, direct reconciliation and all four entity/consolidated reports.
Lifecycle file validation, registry/maintenance, complete CLI extraction and MCP
remain tracked in [implementation progress](implementation/parity-progress.md).
