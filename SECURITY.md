# Security policy

## Supported versions

Books is experimental. Security fixes are provided only for the latest tagged
`0.x` release and the current `main` branch.

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

- Books provides a local CLI and an optional authenticated company-scoped API.
  The API boundary is described below; it is not a hosted multi-tenant service.
- Databases, attachments, plans, and backups are plaintext local files. Books
  relies on operating-system permissions and, where needed, full-disk
  encryption for confidentiality.
- The audit log is an in-database SHA-256 hash chain. It detects accidental,
  partial, and uncoordinated changes. It is not nonrepudiation and cannot resist
  a privileged party that can rewrite and replace the whole SQLite file.
- Imports are untrusted input. Supported parsers enforce resource limits, but
  imports should still be processed in a constrained environment when their
  origin is unknown.
- Supported writes use the Books CLI or authenticated API through shared
  application and ledger services. Direct SQLite writes are
  unsupported and can invalidate accounting and audit guarantees.
- Users are responsible for offline or independently protected backups and for
  professionally reviewing accounting outputs.

## Public-data warning

Never attach a Books database, backup, bank statement, transaction export,
account number, tax record, balance, plan generated from real data, or private
company record to a public issue or pull request. Use minimal synthetic evidence.

## Headless API boundary

The experimental v1 service uses operator-provisioned high-entropy bearer
credentials stored as SHA-256 digests, with read/import/post grants per company
and an additional manage grant in server configuration v2. Chart/default changes, reopen and period close require
manage; year-close posting and changes to closing journals require manage and
post. Registry creation, migrations, backup/restore and local-file imports remain
local administrative operations.
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
