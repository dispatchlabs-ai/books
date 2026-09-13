# Books README: agent-led onboarding

Research date and evidence cutoff: September 13, 2026.

**Decision-ready for the documentation structure.** Lead with bookkeeping through an agent, give the reader one prompt and a visible result, then explain the accounting foundation. Move the command cookbook into operational guidance and reference documentation. An increase in adoption remains an untested hypothesis.

This is a research proposal, including draft copy, rather than a shipped onboarding workflow. It reviews Books at commit `4cdb44f9fed65af840af8beaba17c9ac179af3a9`. The README and runtime have not been changed by this report. The final evidence and quality judgment was made by the researching assistant.

## The decision

Books should answer three questions immediately: What can my agent do with this? How do I try it? What will I be able to inspect afterward?

Use this positioning:

> **Bookkeeping with your AI agent.**
>
> Books is an open-source accounting engine your agent can operate. Ask it to import statements, reconcile accounts, and prepare financial reports. Books stores the ledger locally, enforces balanced entries, and preserves an audit trail.

The reader brings an agent with documentation, shell, and file access. Books supplies the accounting software. This distinction belongs beside the quickstart, because Books does not supply a chat application or a bundled model. The CLI works locally without starting the optional HTTP service. [Books README](../../README.md), [CLI](../cli.md), [API](../api.md).

## What the current README makes difficult

The 331-line README starts with author credit, dense descriptions, and a large feature and format inventory. Its first action is building software. Readers then encounter company initialization, account creation, transaction entry, reconciliation, and reporting as terminal operations. This accurately exposes functionality, but asks the prospective user to imagine the agent experience themselves. [Reviewed README](https://github.com/dispatchlabs-ai/books/blob/4cdb44f9fed65af840af8beaba17c9ac179af3a9/README.md).

Most of that material remains useful. The problem is its placement and audience: task prompts and recognizable results should introduce the product; detailed command syntax should support the agent or an experienced operator. The root `AGENTS.md` is contributor guidance, including development-only database restrictions. It should not become the end-user bookkeeping manual. [Contributor guidance](../../AGENTS.md).

## Evidence and comparisons

These are documentation examples, not a ranking of competing accounting products. Each selected original was opened on the research date. Repository pages are mutable; observations describe the retrieved pages, not permanent product guarantees.

| Source | Observed evidence | Implication for Books | Confidence and boundary |
| --- | --- | --- | --- |
| [GitHub: About READMEs](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-readmes) | Identifies purpose, usefulness, getting started, help, and maintainers as normal README content; recommends keeping longer documentation elsewhere. | Make the README a short entry point with clear navigation. Preserve maintainer and license information. | High for platform guidance; does not prescribe an agent-first design. |
| [FastAgent README](https://github.com/fastagent-sh/fastagent) | Its installation section offers a short prompt directing a coding agent to a separate Markdown setup guide, alongside human installation. | Adopt the handoff pattern: one user instruction plus a substantive agent guide. | High for observed pattern; no evidence that the pattern caused adoption. This is `fastagent-sh/fastagent`, not another similarly named project. |
| [FastAgent setup guide](https://fastagent.sh/start.md) | The destination contains project inspection, path selection, authentication, initialization, and verification instructions. | A prompt is useful because its destination does real work. Books needs an operational guide, not just a shorter slogan. | High for inspected sections; FastAgent's runtime and authentication differ from Books. |
| [Google Workspace CLI](https://github.com/googleworkspace/cli) | Describes both human and agent benefits, provides agent skills, and keeps installation and authentication requirements explicit. | Explain what an agent can accomplish and what setup still entails. Link task guidance to the underlying interface. | High for observed documentation; do not transplant its integrations or installation methods into Books. |
| [agent-browser](https://github.com/vercel-labs/agent-browser) | An explicitly agent-oriented tool still leads with installation and a command-based quickstart. | Command-first documentation remains a reasonable operator reference. Agent orientation alone does not prove prompts are always the best entry point. | High for the inspected opening; no adoption inference from repository popularity. |
| [Diátaxis: Tutorials](https://diataxis.fr/tutorials/) | Recommends a meaningful achievable activity, visible results, clear expectations, and limited distracting explanation. | Give the first Books session a small complete outcome and show how to recognize success. | High for documentation guidance; applying it to a delegated agent workflow is editorial inference. |
| [Diátaxis: Reference](https://diataxis.fr/reference/) | Separates technical descriptions from lessons and task guidance. | Keep accurate CLI/API reference available instead of compressing all details into introductory copy. | High for the documentation distinction. |
| [Anthropic Agent SDK quickstart](https://code.claude.com/docs/en/agent-sdk/quickstart) | Describes an autonomous outcome while still specifying prerequisites, installation, and authentication. | Delegation should reduce user effort without concealing environmental requirements. | High for inspected setup sections; this is an SDK example, not a Books dependency. |

The strongest support is the combination of a directly observed prompt handoff and established documentation guidance. The counterexamples argue for retaining an accessible manual path. They do not invalidate making the user's preferred agent workflow the primary path.

## Recommended README order

Aim for roughly 100–160 lines as an editorial starting point, not a proven optimal length. The first screen should communicate the category, the action to take, and the expected result. Exact screen length depends on the device.

1. **Title and promise.** Books, the headline above, two explanatory sentences, compact experimental status and author credit.
2. **Try it with your agent.** One primary prompt, a short requirements sentence, and the expected result. Link manual installation nearby.
3. **What you can ask.** Three concrete requests: import and review a statement; reconcile an account and explain differences; produce a P&L and identify unresolved items. Describe supported work rather than listing every command or file format.
4. **Why Books.** Local ledger ownership, exact double-entry accounting, immutable posted records with corrections, and inspectable evidence. Translate each property into a reader benefit.
5. **How it works and current scope.** A short explanation of agent → CLI or API → ledger → reports. State experimental maturity, supported platforms and accounting scope, and the most consequential omissions.
6. **Documentation and project.** A small task-oriented link table, contribution and security links, MIT license, and maintained author attribution.

Avoid an elaborate diagram, invented interface screenshot, compatibility badge wall, or long feature checklist. A small verified report example would demonstrate the product more directly. Label invented records as a demo; do not present a proposed exchange as a recorded agent session.

## Draft quickstart copy

The following is candidate copy to test, not a claim that a fresh agent session has already completed it. It uses existing documentation URLs. Once a dedicated operational guide ships, replace the two-document handoff with that guide's URL.

### Try Books with your agent

Use an agent that can read documentation and run commands on your Mac or Linux machine. Books currently installs from source and requires Go 1.26.6 or newer, CGO, and a C compiler; your agent can check the prerequisites and handle the documented setup.

Give your agent this:

```text
Read https://github.com/dispatchlabs-ai/books and its docs/cli.md.
Install Books using the documented method and show me a demo with invented data.
Create a separate USD company called Example Studio for September 2026.
Record $1,000 of received consulting revenue and a $50 software expense.
Show me the profit and loss, trial balance, and accounting health checks.
Keep the demo separate from existing Books data and tell me where it is stored.
```

Expected result: a demo company with **$1,000 revenue, $50 expenses, and $950 net income**, a balanced trial balance, and the results of the accounting checks. These numbers are the intended acceptance criteria, not captured runtime output.

Then try your own statement:

```text
Help me set up Books for my company and review this statement.
Ask for any missing company or account details. Preview the import,
explain the proposed treatment and unresolved items, and wait for me
to approve this batch before applying it. Then show the reconciliation
and reports with their actual coverage dates.
```

The second prompt expresses the user's chosen review boundary for that batch. It must not become a claim that Books enforces human approval for every write. Routine transaction commands can post immediately; import preview/application and draft workflows have their own documented behavior. [CLI contract](../cli.md), [API workflow](../api.md).

## Supporting documentation to ship with a rewrite

Create one small `docs/agent-workflows.md` as the operational entry point. This is a proposed file, not an existing feature. It should route agents through these steps:

- Determine whether the user wants an isolated demo, a new company, an existing company operation, or a report. Preserve existing configuration and follow the requested scope.
- Check platform, source-install prerequisites, installed version, and executable availability. Record the source commit used. Do not invent a package manager installer.
- For the demo, explicitly isolate `BOOKS_HOME` and `BOOKS_CONFIG`, set `BOOKS_ACTOR`, use invented data, establish the fiscal period and needed accounts, and invoke documented commands.
- For real work, establish company, currency, accounting basis, source coverage, account mapping, and the user's requested review boundary. Distinguish importing evidence from posting journals and reconciliation.
- Verify with appropriate reports and health checks. Report unresolved items and paths to output without equating a balanced ledger with correct classification.
- Link to the relevant reference sections for recovery and errors. Distinguish safe retries from operations that require investigation.

Keep this guide short and link to existing reference material. Do not require users to install a new skill package, MCP server, or custom agent integration merely to follow the first workflow; those are separate product decisions.

| Current README material | Recommended destination |
| --- | --- |
| Build prerequisites and source installation | Compact requirements in README; full procedure in the operational guide, linked to contributor build guidance as appropriate |
| Company setup, chart, posting, correction, and fiscal periods | Operational guide for sequencing; [CLI reference](../cli.md) for exact syntax |
| Reconciliation and close | Task guidance linked to [banking data](../banking-data.md) and CLI contracts |
| QuickBooks initial import | [Migration guide](../migration.md) |
| Detailed format inventory | [Statement formats](../statement-formats.md) |
| Configuration and automation contracts | [CLI](../cli.md) and [API](../api.md) |
| Backup and restore | [Operations](../operations.md) |
| Accounting guarantees | Short reader-facing explanation plus [architecture](../architecture.md) |

Inspect destination coverage before removing anything. Moving a section is not permission to lose an existing supported procedure.

## Credibility and OSS alignment

Keep the source-only, experimental status visible. Preserve the MIT license and creator attribution. State macOS/Linux support, single-currency accrual accounting, installation requirements, one working example, and its expected outcome. These requirements fit comfortably in a shorter README.

Avoid calling Books a complete QuickBooks replacement, an autonomous accountant, or a service that automatically connects every bank. Describe the optional HTTP API as an integration surface. Avoid implying a bundled chat UI, bank feed, payroll service, or tax filing feature. [Reviewed product boundary](https://github.com/dispatchlabs-ai/books/blob/4cdb44f9fed65af840af8beaba17c9ac179af3a9/README.md#product-boundary).

Explain the data boundary briefly: Books stores its ledger locally; an external agent's access to financial files and its model provider determine what that agent may transmit. Local storage alone does not establish that data stays off cloud model services. Point readers to the [security policy](../../SECURITY.md) for software boundaries.

## Validation before publishing the new quickstart

Run the exact user-facing prompt in a fresh agent session on a clean supported environment. Test installation, the expected demo reports, health checks, and isolation from existing data. Repeat on the other supported OS. If naming specific agent products as verified, test each named path; capability-based wording is more accurate until then.

Capture the source revision, agent and environment, elapsed time, user interventions, unexpected prompts, failures, and final artifacts. Ask a new reader to identify what Books does and start the demo without explaining the README to them. Use that observation to revise the copy. Do not promise a setup time before measuring it.

This test establishes whether the proposed handoff works. It does not establish adoption or conversion improvement. Evaluate those separately if they become a decision criterion.

## Research method, gaps, and stopping judgment

Scope: public README positioning and onboarding for the Books accounting repository, not financial advice, an accounting-product market study, or a survey of model capabilities. Subquestions were current product truth (repository contracts), useful onboarding patterns (original project docs), information structure (primary documentation guidance), and evidence against the proposed approach (command-first examples and explicit setup requirements).

Exa was the first current-web search. Discovery covered agent-oriented README quickstarts and documentation guidance. Selected originals were fetched directly and compared with Books' repository documentation. Duplicate repository URLs, forks, and historical snapshots were treated as one lineage rather than independent corroboration. Private project context was used only to resolve scope; public product claims here cite the public repository.

The explicit missing-evidence search was `"README" "agent" "onboarding" "conversion" study`. Its returned leads did not establish a controlled comparison of prompt-led versus command-led README adoption. Unverified percentage claims in third-party research summaries were excluded. This is a limitation of the retrieved evidence, not a claim that no such study exists.

The contrary search was `"agent" "quickstart" "prerequisites" "authentication" README`. The selected original SDK quickstart and Workspace CLI documentation confirmed that autonomous outcomes still require concrete setup. The agent-browser opening supplied an additional command-first counterexample.

The assistant's audit found enough original evidence to recommend the structure, with high confidence in observed documentation patterns and moderate confidence in the editorial recommendation. No causal adoption claim is supported. End-to-end Books prompt testing remains necessary before calling the proposed quickstart verified. Further general examples are unlikely to reverse the structure decision, so source expansion stopped here.
