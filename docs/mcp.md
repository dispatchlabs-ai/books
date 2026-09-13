# Books MCP

Books now includes an experimental stdio MCP adapter. It calls the shared backend
without an HTTP server or network listener. Full parity is still in progress:
registry administration, backup/restore and migration remain pending. See the
[progress tracker](implementation/parity-progress.md) for the current gaps.

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
optionally `manage`). This grants access to every book/entity in that file.
Company grants do not imply database access. The private policy is copied at
startup and cannot be changed by tool arguments; restart to change grants.

Tools are named `books_company_<operation_id>` and `books_db_<operation_id>`.
Each accepts `company` or `database` plus a typed `input` object. Discovery omits
tools unavailable under the launch policy. The backend checks grants again on
every call. Mutating tool hints are conservative; annotations never grant access.

Input schemas distinguish decimal amount strings used by convenient workflows
from exact int64 minor-unit strings used by the ledger. Results use exact int64
strings and preserve JSON evidence numbers. Never round these through a floating
point number. Successful results appear under `result` or a scoped `artifact` reference; tool errors set `isError`
and include a stable code. Check `valid`, errors and dry-run fields in results.
Idempotency keys are part of workflows that support them; reuse the same key and
payload after an uncertain response instead of creating another transaction.

The stdio frame limit is 4 MiB and decoded operation input is bounded to 2 MiB.
Bounded file transfer and large-result references are described in [artifacts](artifacts.md). QuickBooks and lifecycle evidence use authorized bundles; maintenance workflows remain pending.
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
