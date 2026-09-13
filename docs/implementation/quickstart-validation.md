# Agent quickstart validation

Two independent fresh-agent sessions ran the exact README prompt on September 13,
2026, one on macOS and one on Linux. Each fetched the public operational guide,
installed Books with its documented `go install ...@main` method, and executed
the isolated Example Studio demo. Neither received implementation-task history.

Both installed `v0.0.0-20260913193323-cf8dfcae8ea0`, corresponding to
[commit cf8dfca](https://github.com/dispatchlabs-ai/books/commit/cf8dfcae8ea0ab5c057041a2dddf0dbba2e172ef).
The prompt and demo commands were unchanged during these tests.

| Environment | Setup observed | Result |
| --- | --- | --- |
| macOS, Darwin arm64 | Go 1.26.2 and C compiler present; documented automatic toolchain selection downloaded Go 1.26.8 | Passed; no human intervention |
| Linux, x86_64 | GCC 16.2.1 present; Go absent from normal PATH. Agent installed checksum-verified Go 1.27.1 into temporary storage using official installation guidance | Passed; no human intervention |

Both runs produced September 2026 revenue of **$1,000.00**, expenses of **$50.00**,
and net income of **$950.00**. Trial balance debits and credits were **$1,000.00**
each: Checking debit $950.00, Software debit $50.00, Consulting Revenue credit
$1,000.00. Doctor reported integrity `ok`, zero foreign-key violations and zero
unbalanced journals. Audit verification was valid with 28 events.

Every installation/demo command exited successfully; no Books command correction
was required. The Linux compilation emitted two SQLite C const-qualifier warnings
without failing installation. The macOS agent used absolute executable paths
rather than relying on PATH across shell calls. Both isolated Go caches and
installation directories; the Linux toolchain installation changed no persistent
configuration or system binary.

The test harness restricted each agent to new temporary installation/data paths
and prohibited existing Books data access and repository changes. Every accounting
command used isolated `BOOKS_HOME`, `BOOKS_CONFIG`, and `BOOKS_ACTOR` with `BOOKS_DB`
unset. Agents returned the actual demo paths and left the synthetic records
available for inspection; temporary storage can be removed by the OS.

This verifies the published prompt in these two shell-capable agent sessions,
including prerequisite handling. It does not measure adoption, certify a graphical
MCP client, promise compatibility with every agent, or test every compiler/OS
combination. The demo does not import statements, reconcile bank activity or close
periods. Those outcomes cannot be inferred from its passing integrity checks.

Separately, the maintainer-hosted Linux CI runner's receipt and GitHub status both
reported success for the same source commit. Automatic CI remains enrolled;
GitHub Actions is disabled. See [contributor checks](../../CONTRIBUTING.md).
