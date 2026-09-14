# Security policy

## Supported versions

Books is experimental and currently has no tagged releases. Security fixes are
provided on the current `main` branch.

## Reporting a vulnerability

Do not open a public issue. Use GitHub's private vulnerability-reporting form:

https://github.com/dispatchlabs-ai/books/security/advisories/new

Include a concise description, affected command or file format, reproduction
steps using synthetic data, impact, and any proposed mitigation. Do not include
real databases, statements, account identifiers, credentials, or financial
records.

The maintainers will acknowledge a complete report as availability permits.
This experimental project does not currently promise a contractual response or
remediation time.

## Security boundary

- Books provides a local CLI, an optional authenticated HTTP API, and a built-in
  stdio MCP server. Their authorization boundaries are described below; this is
  not a hosted multi-tenant service.
- Databases, attachments, plans, and backups are plaintext local files. Books
  relies on operating-system permissions and, where needed, full-disk
  encryption for confidentiality.
- The audit log is an in-database SHA-256 hash chain. It detects accidental,
  partial, and uncoordinated changes. It is not nonrepudiation and cannot resist
  a privileged party that can rewrite and replace the whole SQLite file.
- Imports are untrusted input. Supported parsers enforce resource limits, but
  imports should still be processed in a constrained environment when their
  origin is unknown.
- Supported writes use the Books CLI, authenticated API, or policy-bound MCP
  tools through shared application and ledger services. Direct SQLite writes are
  unsupported and can invalidate accounting and audit guarantees.
- Users are responsible for offline or independently protected backups and for
  professionally reviewing accounting outputs.

## Public-data warning

Never attach a Books database, backup, bank statement, transaction export,
account number, tax record, balance, plan generated from real data, or private
company record to a public issue or pull request. Use minimal synthetic evidence.

## Headless API boundary

The experimental v1 service uses operator-provisioned high-entropy bearer
credentials stored as SHA-256 digests. Company permissions are `read`, `import`,
`post`, and (configuration v2+) `manage`. Chart/default changes, reopen and period
close require manage; year-close posting and changes to closing journals require
manage and post.

Configuration v3+ can grant whole-database `read` and `manage`, covering every
entity and book in the selected file. Database manage includes posting and is
broader than company manage. Configuration v4 adds separate registry read/manage
and database admin for initialization, migration, backup and restore. Company
access does not imply either authority. An explicit company `*` grant includes
future registrations; exact company entries override it. Existing databases bind
to configured UUIDs. An uninitialized target may omit its UUID only with admin;
pin the returned identity for ongoing enforcement.

Clients select configured database handles, never arbitrary server paths.
Registry operations change only the supported preferences and registrations;
registry readers can see registered storage paths. Restoration requires the exact
database handle as confirmation and reuses lineage and recovery checks. See
[administration and maintenance](docs/api.md#registry-and-database-administration).
Grants and tokens reload on process restart. Non-loopback listeners require TLS;
browser access requires exact configured origins. There is no cookie auth,
public signup, OIDC, or distributed tenant isolation. All authenticated readers
of a company can download its complete uploaded source files.

Raw files and parsed account identifiers are stored unencrypted in SQLite and
its backups. Use OS access controls and encrypted storage as appropriate.
Upload and parser limits constrain individual requests; operators remain
responsible for disk capacity and authenticated-client usage. The service does
not log bearer credentials or request bodies. Do not distribute a shared posting
credential in a public web bundle. See [client configuration](docs/api.md).

## MCP and file boundary

`books mcp` communicates over stdin/stdout and opens no network listener. Its
private operator-owned policy supplies the actor, registry path, database handles
and explicit grants. Tool discovery filters by permission; shared execution
checks authority again. Tool arguments cannot change policy, impersonate an actor,
run SQL/shell commands or expand filesystem access. Restart to change policy.
See [MCP setup](docs/mcp.md) for the implemented configuration.

Artifacts require an operator-selected private directory. They bind to actor and
verified database identity; company artifacts also bind entity/book. Transfers
are bounded, hash-checked and accept opaque IDs rather than arbitrary paths.
Committed accounting evidence is retained and cannot be discarded. Preserve that
artifact directory alongside database backups. Uploaded statement sources stored
in the ledger remain readable by authorized company readers; temporary artifact
ownership is narrower. See [file limits and retention](docs/artifacts.md).

The CLI and adapters run with their OS user's authority. Their policy is not an
OS sandbox against another program running as that user. Maintenance locks
coordinate Books processes under the same OS user; stop external SQLite tools
before migration or restoration. Operations retain their documented retry
contracts, not a universal exactly-once guarantee. An external agent's model
provider and file permissions determine where financial information may be sent.

## Experimental web client

The optional [web client](web/README.md) runs a loopback-only single-operator
facade with server-held read credentials and an explicit route allowlist. It
does not provide network-user authentication and must not be exposed publicly.
AI requests go only to an explicitly configured adapter; that adapter owns tool
scope enforcement and authorization. Demo data and browser-local plans are
synthetic. Live forecasts and financial execution are not implemented.
