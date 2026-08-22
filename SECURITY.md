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

- Books is a local, single-user CLI. It does not provide network authentication,
  multi-user authorization, or a hosted service boundary.
- Databases, attachments, plans, and backups are plaintext local files. Books
  relies on operating-system permissions and, where needed, full-disk
  encryption for confidentiality.
- The audit log is an in-database SHA-256 hash chain. It detects accidental,
  partial, and uncoordinated changes. It is not nonrepudiation and cannot resist
  a privileged party that can rewrite and replace the whole SQLite file.
- Imports are untrusted input. Supported parsers enforce resource limits, but
  imports should still be processed in a constrained environment when their
  origin is unknown.
- The supported write boundary is the Books CLI. Direct SQLite writes are
  unsupported and can invalidate accounting and audit guarantees.
- Users are responsible for offline or independently protected backups and for
  professionally reviewing accounting outputs.

## Public-data warning

Never attach a Books database, backup, bank statement, transaction export,
account number, tax record, balance, plan generated from real data, or private
company record to a public issue or pull request. Use minimal synthetic evidence.
