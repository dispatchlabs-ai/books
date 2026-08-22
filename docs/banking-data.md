# Banking Data Standard v1

This standard governs bank, credit-card, loan, and investment evidence entering Books. The Books SQLite database is the authoritative ledger. Provider exports, statement files, and mixed transaction databases are source evidence only.

## Ownership boundary

Every imported file explicitly names one registered company and one statement account. Books never infers ownership from account labels, institution names, masks, merchants, descriptions, amounts, or transaction patterns. External tooling is responsible for separating personal and company evidence before it invokes Books.

Every source account must be mapped by an exact, case-sensitive external ID to one legal entity and one active Books statement account. A replacement identity created by a provider remains unmapped until it is separately reviewed and added. Multiple reviewed identities may converge on one physical statement account without merging their independent source histories.

## Statement-account contract

A Books statement account belongs to one legal entity and one book and controls one active general-ledger account. Its kind is `BANK`, `CREDIT_CARD`, `LOAN`, or `INVESTMENT`. Bank and investment controls are assets; credit-card and loan controls are liabilities. Reconciliation-required start and end dates are policy bounds, not assertions about the provider account's legal life.

Each provider identity is stored with its source system, source realm, exact external ID, evidence kind, evidence path or locator, and SHA-256 digest. Use `GLOBAL` as the realm only when the provider guarantees that its IDs are globally unique.

## Transaction contract

The supported interchange format is JSON conforming to [banking-import-v1.schema.json](schemas/banking-import-v1.schema.json). One file targets one Books statement account and source system. Its raw file SHA-256 becomes import-batch evidence.

`amount` is an exact decimal with no more than two fractional digits and represents the debit-minus-credit change to the mapped general-ledger control:

| Control kind | Positive amount | Negative amount |
| --- | --- | --- |
| Bank | Deposit or other asset increase | Withdrawal or other asset decrease |
| Investment | Contribution, purchase, or other asset increase | Distribution, sale, fee, or other asset decrease |
| Credit card | Payment, credit, or liability decrease | Charge, fee, or liability increase |
| Loan | Principal payment or liability decrease | Proceeds or other liability increase |

Interest, fees, gains, losses, and principal are classified in the journal entry; the statement transaction records the signed change to the control account. Every transaction carries a stable external ID, effective posted date, description, disposition, and preserved raw evidence. Optional tax type and tax accounting period describe evidence classification; they do not post a journal by themselves.

## Lifecycle and idempotency

`POSTED` transactions can enter reconciliation. `PENDING`, `NEEDS_REVIEW`, and `SOURCE_ONLY` rows remain immutable source evidence and do not enter the statement balance. A provider's pending-to-posted change is retained as a new revision of the same logical identity. Any manual resolution from review to posted or source-only requires a reason and structured evidence.

Reimporting the same file is idempotent. A changed observation for the same source identity creates a revision; it never overwrites the prior record. Source collisions are preserved and held for review unless lifecycle evidence distinguishes them.

A pending row retains its original external identity when a later posted row explicitly names it as `pending_transaction_id`; the posted provider ID and predecessor relationship remain in the immutable raw observation. More than one matching identity is a blocker rather than a guessed merge.

## Reconciliation standard

Each reconciliation has an exact start date, end date, statement opening balance, and statement ending balance. Completion requires all of the following:

1. Statement opening balance equals the prior completed statement ending balance when a prior period exists.
2. Opening balance plus signed statement activity equals the statement ending balance.
3. General-ledger opening equals statement opening plus opening outstanding items, and general-ledger ending equals statement ending plus ending outstanding items.
4. Every posted statement transaction and every control-account line attested as cleared is fully allocated using signed, many-to-many allocations; every remaining eligible control-line amount is explicitly retained as reviewed outstanding evidence.
5. The period does not overlap another completed reconciliation for the account.

Outstanding deposits and payments carry into later reconciliation candidates and can be allocated when the provider statement eventually clears them. Manual plans label their generated statement mirrors as operator attestations; they are not represented as independent provider observations.

An incomplete required reconciliation blocks period close, but a completed reconciliation with exact adjusted balances may close while items remain outstanding. Incorrect open work is abandoned with retained audit evidence. A completed reconciliation can be reopened only through the audited CLI workflow, and the short `reconcile replan` workflow must target that exact reopened interval before reclose.

## Operational controls

- Use the Books CLI for every ledger mutation; never write SQLite directly.
- Keep live databases, statements, credentials, exports, and mapping evidence outside Git.
- Run `books db doctor` and `books audit verify` before and after material imports.
- Create a verified backup before cutover, correction, or close.
- Keep each legal entity separate. Consolidated reports use effective-dated ownership and explicit elimination entries; source imports never create cross-entity transactions implicitly.
