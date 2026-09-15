# Books

**Bookkeeping with your AI agent.**

Books is an open-source accounting engine your agent can operate. Ask it to
import statements, reconcile accounts, and prepare financial reports. Books
stores your ledger locally, enforces balanced entries, and preserves an audit trail.

Created by Chris Reynolds, cofounder of [Dispatch Labs AI](https://github.com/dispatchlabs-ai).

**Experimental source software · macOS and Linux · MIT**

## Use it with your AI agent

Start with a small demo: two transactions, a profit and loss statement, and
accounting checks. You supply an agent that can read documentation, run shell
commands, and access local files. Books supplies the ledger and accounting tools.

Copy this into your agent:

```text
Read https://github.com/dispatchlabs-ai/books/blob/main/docs/agent-workflows.md
and follow its demo workflow. Install Books using the documented method.
Create an isolated Example Studio company with invented USD data:
$1,000 of consulting revenue and a $50 software expense in September 2026.
Show me the profit and loss, trial balance, and accounting health checks.
Keep the demo separate from existing Books data and tell me where it is stored.
```

Your result should include:

| Example Studio — September 2026 | Expected result |
| --- | ---: |
| Revenue | $1,000.00 |
| Expenses | $50.00 |
| Net income | **$950.00** |

The trial balance should balance, and Doctor and audit verification should pass.
These are synthetic demo records. The [agent guide](docs/agent-workflows.md)
provides the exact workflow and explains what these checks establish.
[Fresh-agent validation](docs/implementation/quickstart-validation.md) records
successful macOS and Linux runs, including prerequisite setup.

Installation currently requires **Go 1.26.6 or newer, CGO, and a C compiler**.
Your agent can check prerequisites and follow the setup guide. There is no tagged
release or published binary yet. Prefer the terminal? Start with
[manual installation and workflows](docs/manual-workflows.md#build).

## What you can ask

Once you have tried the demo, give your agent a real task and the files it needs:

- **Review a statement:** “Help me set up my company and preview this statement
  import. Explain the proposed treatment and unresolved items before applying it.”
- **Reconcile an account:** “Reconcile Checking through the statement ending date
  and explain any difference or outstanding transactions.”
- **Understand the books:** “Prepare my profit and loss and balance sheet. State
  the dates covered and flag unresolved items that affect the results.”

Your agent handles the documented commands and asks for missing business details.
Statement import, journal posting, and reconciliation are distinct steps;
importing a file alone does not establish that the books are complete.
See [working with real books](docs/agent-workflows.md#working-with-real-books).

## Why Books

- **Own your ledger.** Company data lives in local SQLite files, with backup and
  restore tools. The CLI runs without a server.
- **Inspect the accounting.** Amounts use exact integer minor units. Posting
  requires balanced entries, valid accounts, and an open period.
- **Preserve history.** Posted journals stay immutable; corrections create linked
  reversals. Reconciliation and close workflows retain evidence for inspection.
- **Build on the same engine.** Agents can use the noninteractive CLI; client
  applications can use the authenticated HTTP API, and agents can use the built-in
  [stdio MCP server](docs/mcp.md). All three use shared accounting services,
  including explicitly authorized company and database administration.

Balanced entries and passing checks do not prove that a classification is correct.
Books is not accounting advice; have accounting outputs reviewed before relying
on them. The [architecture](docs/architecture.md) and
[security policy](SECURITY.md) explain the guarantees and their limits.

## Current scope

Books supports single-currency accrual books for a company or person: company
setup, accounts, transaction entry, statement imports, reconciliation, financial
reports, period and year close, consolidation, and optional initial QuickBooks
migration. See [statement formats and limits](docs/statement-formats.md) and
[currency rules](docs/currencies.md).

Books includes an experimental [shadcn web interface](web/README.md) for
overview, account activity and an optional AI adapter. Its connected view supports [daily cash projections](docs/cash-projections.md)
from explicit evidence-backed scenario files. Books does not include a model, automatic bank feeds,
invoicing or A/R and A/P workflows, bill pay, payroll processing, inventory, or
tax filing. Multiple currencies within an entity, currency translation, and
partial ownership are unsupported. Automatic plan discovery and offline synchronization remain future work.

Books does not encrypt its local databases, attachments, plans, or backups.
Protect these files and keep independent backups. Your agent's model provider
and file access determine what it may send to external services; a local ledger
alone does not keep agent conversations local. Never post real financial data
in a public issue.

## Documentation

| I want to… | Start here |
| --- | --- |
| Use Books through my agent | [Agent workflows](docs/agent-workflows.md) |
| Install and operate Books manually | [Manual workflows](docs/manual-workflows.md) |
| Look up commands and automation behavior | [CLI reference](docs/cli.md) |
| Connect an MCP agent | [MCP setup and permissions](docs/mcp.md) |
| Build a client or import statements through the API | [API](docs/api.md) |
| Understand supported statement data | [Formats](docs/statement-formats.md) · [Banking data](docs/banking-data.md) |
| Back up or recover a company | [Operations](docs/operations.md) |
| Migrate existing books | [Migration](docs/migration.md) |
| Understand the ledger | [Architecture](docs/architecture.md) |

## Project

Read [CONTRIBUTING.md](CONTRIBUTING.md) to contribute and [AGENTS.md](AGENTS.md)
when using a coding agent to develop Books. Report bugs with synthetic examples
through [GitHub Issues](https://github.com/dispatchlabs-ai/books/issues); report
vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

Books is licensed under the [MIT License](LICENSE).
