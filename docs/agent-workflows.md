# Using Books with an agent

This is the operational entry point for an agent helping someone use Books.
Start with [the README](../README.md) for the product's scope. For changes to
Books' source code, follow [contributor guidance](../AGENTS.md) instead.

## Choose the task

- **Demo:** follow the isolated example below with invented records. No financial
  files, bank credentials, server, or real company configuration are needed.
- **New company:** collect the company name, currency, fiscal year, and intended
  source coverage before initializing it.
- **Existing company or report:** use the user's selected configuration and
  company. Confirm the target before mutations; do not create a replacement
  company or change the default just to make a command succeed.

An agent needs documentation access, a shell on macOS or Linux, and access to
files the user chooses. Books has no built-in model or chat interface. Respect
the user's authorization and the agent environment's permissions. Use existing
authorization for routine steps; ask for missing details that affect the result.

## Install from source

Check `go version`, `go env CGO_ENABLED`, and that a C compiler such as `cc` is
available. Go 1.26.6 or newer and CGO are required by the SQLite driver. If a
prerequisite is missing, use the platform's documented installation method
within the user's authorization, or explain the specific blocker. Do not claim
that installing Books also installs an agent or model subscription.

Install in a new directory so an existing Books executable is not replaced:

```sh
books_install=$(mktemp -d "${TMPDIR:-/tmp}/books-install.XXXXXX")
GOBIN="$books_install" CGO_ENABLED=1 GOTOOLCHAIN=auto \
  go install github.com/dispatchlabs-ai/books/cmd/books@main
export PATH="$books_install:$PATH"
books --version
go version -m "$books_install/books"
```

Check each command's exit status and stop on failure. Record the installed source
version from the build information. There are no tagged releases or published
binaries yet; replace `main` with a chosen commit for reproducible installation.
If using a source checkout, the alternative is `go build -o dist/books ./cmd/books`
with CGO enabled, then use that executable explicitly. See
[manual installation](manual-workflows.md#build).

## Demo workflow

The README prompt requests this workflow. Keep the environment variables and
`PATH` in effect for every command, even if the agent uses a new shell for each
call. Run one step at a time and check results before continuing. Do not access
an existing Books database for this demo.

Create a fresh directory and explicitly isolate both data and configuration:

```sh
books_demo=$(mktemp -d "${TMPDIR:-/tmp}/books-demo.XXXXXX")
export BOOKS_HOME="$books_demo/home"
export BOOKS_CONFIG="$BOOKS_HOME/books.toml"
export BOOKS_ACTOR="agent-demo"
unset BOOKS_DB

books init --name "Example Studio" --company example-studio \
  --currency USD --basis accrual --start 2026-01-01 \
  --fiscal-year-end december
books --company example-studio account add bank Checking \
  --default-payment --default-deposit
books --company example-studio account add income "Consulting Revenue"
books --company example-studio account add expense Software

books --company example-studio receive 1000.00 "Consulting Revenue" \
  "Synthetic consulting receipt" --to Checking --date 2026-09-10 \
  --key example-studio-demo-revenue
books --company example-studio spend 50.00 Software \
  "Synthetic software expense" --from Checking --date 2026-09-11 \
  --key example-studio-demo-software

books --company example-studio pl --from 2026-09-01 --to 2026-09-30
books --company example-studio tb --as-of 2026-09-30
books --company example-studio doctor
books --company example-studio audit verify
```

The fiscal year starts in January; the example transactions and P&L are in
September. Explicit dates keep the example independent of today's date.

Verify the actual output:

- Revenue is $1,000.00, expenses are $50.00, and net income is $950.00.
- The trial balance has $1,000.00 of debits and $1,000.00 of credits:
  Checking has a $950.00 debit, Software a $50.00 debit, and Consulting Revenue
  a $1,000.00 credit. Zero-balance starter accounts may also appear.
- Doctor and audit verification succeed. This demo does not import a statement,
  reconcile the bank account, or close the period. Those outcomes must not be
  inferred from passing integrity checks.

For structured output, add `--json` to an invocation and read the `books.cli/v1`
envelope. CLI money values are exact decimal strings. Report actual results and
any failed checks, not just the expected numbers.

The transaction keys permit retrying those same transactions without duplication.
Do not blindly rerun initialization or account creation in a partially completed
demo. Inspect only this demo's configuration and accounts, then resume the missing
steps, or create a new isolated demo directory.

End with the P&L summary, trial balance totals, health-check results, source
version, and the actual `BOOKS_CONFIG` and company data paths. Explain that the
records are invented. Leave the demo available for inspection and explain that
its temporary location may be cleaned by the OS; do not silently move it into the
user's real books. Remove only directories created for this task if the user asks
for cleanup.

## Working with real books

Read the relevant [CLI reference](cli.md), [manual workflows](manual-workflows.md),
and [operations guide](operations.md) before the first real operation. The demo's
disposable paths and synthetic accounts are not defaults for a real company.

1. Establish the target configuration and company, currency, fiscal year, and
   requested task. Use `--company` explicitly. Books currently supports accrual
   accounting only. Read [currency rules](currencies.md) when choosing currency
   or importing amounts. Do not guess opening balances or accounting treatment.
2. Establish which files the user wants processed and their coverage dates.
   Check [statement formats](statement-formats.md) and the
   [banking data contract](banking-data.md). Some tabular formats require a mapping
   profile. QuickBooks migration has a separate [migration guide](migration.md).
3. Follow the supported workflow for the input. The [API guide](api.md) documents
   statement upload, mapping, preview, and application; the CLI references
   document command-based imports. Do not invent CLI equivalents of API endpoints
   or start a server for a workflow that the local CLI already supports.
4. Distinguish evidence import, classification, journal posting, and reconciliation.
   Explain proposed treatment and unresolved items. Follow the user's requested
   review boundary before applying a batch; supported preview and draft options
   are useful here. Routine `spend`, `receive`, and `transfer` commands post
   immediately unless `--draft` or `--dry-run` is used. Books does not enforce
   universal human approval.
5. Use stable idempotency keys for transaction mutations and the documented retry
   contract for other operations. Apply the exact reviewed plan where a workflow
   provides one. A stale or rejected plan needs investigation or regeneration;
   changing the plan file to bypass a check is not a retry strategy.
6. Reconcile against actual statement evidence, including outstanding items, and
   produce the requested reports with explicit dates. Current reports do not
   require a routine period lock. Perform close or reopen operations only when
   they are part of the requested work.
7. Run appropriate integrity checks and report actual coverage, unresolved items,
   and evidence locations. Balanced journals do not prove correct classification
   or complete source coverage. Use linked reversals for corrections; never edit
   SQLite directly. Follow the operations guide for backups and recovery.

## Errors and data boundaries

The CLI is noninteractive. Missing or ambiguous inputs produce a nonzero exit
status; in machine mode, inspect the stable error code in the failure envelope.
Books reads stdin only when explicitly given `--input -`. A failed plan may still
leave a plan file for review; existence of that file does not prove success.

Use only the documented CLI or authenticated API to write accounting data.
Keep credentials and financial records out of source control and public issues.
Books does not encrypt its databases, attachments, plans, or backups. An external
agent may send information to its model provider according to its configuration;
local storage alone does not establish that financial data stays on the machine.
See [SECURITY.md](../SECURITY.md) for the software's boundaries.
