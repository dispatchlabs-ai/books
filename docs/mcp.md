# Books MCP

Books includes an experimental stdio MCP adapter covering existing bookkeeping,
reporting, evidence, company/configuration and database administration operations.
It calls the shared backend without an HTTP server or network listener. See the
[coverage and validation record](implementation/parity-progress.md).

Launch with `books mcp --policy /absolute/path/books-mcp.json`. The policy must be
a private regular file (mode 0600), with explicit actor and grants. MCP ignores
ambient Books registry selection and output defaults; do not pass CLI database,
company, actor, format or dry-run flags to the server. Tool requests carry their
own declared scope and supported dry-run fields.

```json
{
  "schema": "books.mcp-policy/v1",
  "actor": "bookkeeping-agent",
  "config_path": "/absolute/path/books-home/books.toml",
  "artifact_directory": "/absolute/path/books-home/artifacts",
  "companies": {"example": ["read", "import", "post", "manage"]},
  "databases": {}
}
```

Use a real registered company key and config path. For whole-database tools, add
an entry under `databases` with `path`, verified `uuid` and `grants` (`read` and
optionally `manage` for bookkeeping and `admin` for maintenance). This grants access to every book/entity in that file.
Company grants do not imply database access. The private policy is copied at
startup and cannot be changed by tool arguments; restart to change grants.

Tools are named `books_company_<operation_id>`, `books_db_<operation_id>` and
`books_registry_<operation_id>`. Registry tools accept only `input`.
`books_health` and `books_capabilities` accept an empty object.
Company/database tools accept `company` or `database` plus typed `input`. Discovery omits
tools unavailable under the launch policy. The backend checks grants again on
every call. Mutating tool hints are conservative; annotations never grant access.

Input schemas distinguish decimal amount strings used by convenient workflows
from exact int64 minor-unit strings used by the ledger. Results use exact int64
strings and preserve JSON evidence numbers. Never round these through a floating
point number. Successful results appear under `result` or a scoped `artifact` reference; tool errors set `isError`
and include a stable code. Check `valid`, errors and dry-run fields in results.
Idempotency keys are part of workflows that support them; reuse the same key and
payload after an uncertain response instead of creating another transaction.

The stdio frame limit is 4 MiB and inline operation input is bounded to 2 MiB. Larger company/database JSON inputs
can use `input: {"input_artifact":"<id>"}` (up to 256 MiB).
Bounded file transfer and large-result references are described in [artifacts](artifacts.md). QuickBooks and lifecycle evidence use authorized bundles; maintenance uses the same artifact transfer.
No client-supplied roots, credentials, SQL, shell commands or server paths can
expand policy. The process runs as its OS user; this is application authorization,
not isolation from another program with that user's filesystem authority.

The official Go MCP SDK v1.7.0 supplies protocol lifecycle, cancellation and tool
negotiation. The compiled Books process is tested with the SDK client over actual
stdio, including discovery, create/validate/post/report and scope denials.
This is not yet a tested configuration for a named graphical agent client.

Dependency rationale: using the maintained
[official SDK](https://github.com/modelcontextprotocol/go-sdk/tree/v1.7.0) avoids
implementing JSON-RPC/MCP lifecycle by hand. Dependencies are pinned, license
notices are retained in [third-party notices](../THIRD_PARTY_NOTICES.md), and the
canonical check runs dependency integrity and vulnerability checks. The transitive
`x/sys` version is raised to v0.44.0 to remove GO-2026-5024, even though its affected
Windows package is not used on supported Books platforms.

## Setup and administration

To let an agent create companies, add `"registry": ["read", "manage"]` to its
policy. Registry tools include `company_add`, `company_default`, `company_list`,
`config_get`, `config_path` and `config_set`. Paths are operator-selected;
`config_set` only changes the existing supported preferences and account defaults.
Registry readers can see company registrations and their local storage paths.
Registry metadata results remain inline because they have no database artifact scope.

For explicit access to current **and future** registered companies, use
`"companies": {"*": ["read", "import", "post", "manage"]}`. Exact company entries
override the wildcard. Registry permissions alone do not grant posting access.
An empty registry can start with this policy; create the first company with:

```json
{"input":{"initialize":true,"options":{"key":"example","name":"Example Company","start":"2026-01-01"}}}
```

Call `books_registry_company_add`. Later additions omit `initialize`. Use
`dry_run:true` to preview creation or changing the default company. New companies
are immediately usable under explicit wildcard grants. There is no automatic
expansion of configured whole-database handles: an operator must select those
paths and permissions, including any anticipated new database paths.

The database maintenance tools are `books_db_db_init`, `books_db_db_migrate`,
`books_db_db_backup`, and `books_db_db_restore`. They require database `read` and
`admin`. Uploading backup files additionally requires `manage`. A missing database
target may omit `uuid` only with `admin`; `db_init` then creates it and returns its
identity. Pin that UUID in the policy for ongoing identity enforcement.

Backup takes a stable simple `key` and returns a downloadable artifact. Retry the
same key for the same snapshot; use a new key for a new snapshot. Restore takes
`artifact`, supports `dry_run:true`, and requires `confirm` equal to the exact
database handle for a real restore. Preserve its returned recovery artifact and
all retained source evidence. See [administration](api.md#registry-and-database-administration)
for maintenance coordination and retry limits.
