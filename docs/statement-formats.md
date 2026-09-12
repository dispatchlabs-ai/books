# Bank statement formats

All formats below use the same durable upload, inspection, mapping, preview,
and atomic apply workflow through the [CLI and API](api.md). Run
`books bank-import formats` or `GET /v1/capabilities` for the installed adapter
list. File structure selects the parser; a filename extension is not proof of
format. Select ambiguous delimited input explicitly with upload options.

Books posts accrual accounting in each entity's chosen currency. Adapters retain
source currencies and exact decimal amounts for inspection. A READY
job means parsing succeeded; posting also requires compatible account kind,
currency, precision, mapping, periods, and explicit classifications.

## Adapter profiles

| Format | Supported source profile | Required or conditional options |
| --- | --- | --- |
| OFX | Bank/card SGML 102, 103, 160; XML 200, 201, 202, 203, 210, 211, 220, 230; headerless bank/card XML | Source institution, full account identity, supported currency and settled transactions must be present. |
| QFX / QBO | OFX bank/card Web Connect files, including `INTU.BID` and `INTU.USERID` | Use `format` to label QFX/QBO explicitly; automatic detection labels OFX. These share OFX account and FITID identity. |
| QIF | Bank, Cash, Other Asset and Credit Card registers; account lists, categories, classes, monetary splits | `institution`, `currency`, `date_layout`; `account_id` when no account list supplies a name. Two-digit years need `year_pivot`. |
| CSV / TSV | Quoted delimited records with an explicit column and amount profile | `format`, `institution`, `date_layout`, `tabular`; account and currency in profile options or mapped columns. |
| XLSX | One selected worksheet, shared/inline strings, literal numeric/date cells | Same column profile; `sheet` when the workbook has multiple sheets. Numeric cells use XML numeric notation independently of text locale. |
| CAMT | `camt.052`, `.053`, `.054`, each with namespace `.001.02` or `.001.08` | `institution` if the account servicer has no BIC/other identifier. Entry booking dates and account currency are required. |
| MT940 | Tagged customer statements, opening/closing balances, entries, descriptions and contiguous statement pages | `institution`, `year_pivot`; `mt_booking_date` if an entry omits its booking date. |
| MT942 | Tagged interim reports, report timestamp, floor limits and supplied debit/credit totals | `institution`, `year_pivot`; currency from source or `currency`; missing booking-date policy as for MT940. |
| BAI2 | Version 2 records 01/02/03/16/88/49/98/99; standard cash balance, summary and detail codes | `year_pivot`; declared currency scale. Custom and loan codes fail explicitly. |
| BTRS | X9.121 version 3 record layouts, with the supported standard BAI cash code subset | `year_pivot`; currency must be on the account record. Retired funds type D, custom and unrecognized codes fail explicitly. |
| CODA | Version 2, Febelfin 2.7 layout; movement/information continuations, parent/detail hierarchy, multiple files | `year_pivot`; `institution` if the source lacks a bank identifier; `currency` if absent in a supported account structure. |
| CFONB120 | July 2004, 120-byte records 01/04/05/07, signed overpunch amounts | `year_pivot`; declare a legacy `encoding` when needed. |
| Norma 43 | June 2012, 80-byte account/movement/complement/trailer records, modes 1/2/3 | `year_pivot`; declare a legacy `encoding` when needed, including `CP850` for DOS exports. |

Existing Books JSON statement/journal imports and the specific QuickBooks
JSON/journal-XLSX import remain separate supported commands. This addition
does not reinterpret those files as bank statements.

These are tested adapter profiles, not certification of every bank dialect or
all business features in each standard. Unsupported versions, missing fields,
ambiguous dates/signs, invalid continuations, mismatched control totals, and
unsupported accounting semantics fail with a diagnostic. QIF investment/other
liability registers, OFX pending/correction/investment extensions, EBCDIC files,
Excel formulas/macros/external workbook links, encrypted files, and incomplete
CAMT pagination are not accepted. Multi-page CAMT reports must be assembled into
one complete report before upload. An MT input may contain tagged statement
pages; multiple SWIFT envelopes must be uploaded separately.

## Durable source options

Supply a JSON options file using `bank-import upload --options profile.json`.
HTTP accepts `multipart/form-data` with exactly one `file` field and an optional
`options` JSON field. The original `application/octet-stream` upload remains
available for formats needing no options. Multipart `name` query overrides the
file part's filename. Options are stored in the immutable upload audit evidence,
included in upload idempotency, and carried by database backup/restore. Parsing
never depends on the machine's current locale or current date.

For example, a CSV with `Date,Amount,Description` columns:

```json
{
  "format": "CSV",
  "institution": "EXAMPLE-BANK",
  "account_id": "EXAMPLE-ACCOUNT",
  "account_kind": "BANK",
  "currency": "USD",
  "date_layout": "2006-01-02",
  "tabular": {
    "header_row": 1,
    "date_column": "Date",
    "amount_column": "Amount",
    "description_columns": ["Description"],
    "decimal_separator": "."
  }
}
```

The tabular profile also supports `id_column`, `value_date_column`,
`account_column`, `currency_column`, `status_column` with explicit `status_values`,
`sheet`, `delimiter`, `thousands_separator`, and `invert_sign`. Instead of
`amount_column`, provide both `debit_column` and `credit_column`: at least one
side must be supplied, amounts must be nonnegative, and at most one may be
nonzero. An explicitly supplied zero is valid. A configured ID column must
contain reliable bank identifiers; check numbers and descriptions are not IDs.

Date layouts are literal Go-style layouts: `2006-01-02`, `20060102`,
`01/02/2006`, `02/01/2006`, `01/02/06`, `02/01/06`, `02.01.2006`,
`02-01-2006`, `2006/01/02`, `EXCEL-1900`, or `EXCEL-1904`. A two-digit year
requires `year_pivot`: with 70, years 70–99 mean 1970–1999 and 00–69 mean
2000–2069. Excel serial dates must be integral, match the workbook's date
system, and exclude the fictitious 1900 leap day.

`encoding` is `UTF-8` by default. Supported explicit legacy encodings are
`WINDOWS-1252`, `ISO-8859-1`, and `CP850`. Fixed-width field positions are
measured in source bytes before decoding text. Decimal amounts use exact text,
never floating point. Input amounts are bounded to 30 integral and 9 fractional
digits; posting additionally enforces the owning currency's minor-unit precision and int64 range.

For MT entries without a booking date, `mt_booking_date` must explicitly be
`statement` (report/closing date) or `value`. Use the policy agreed for the bank's
export; value dates are otherwise retained separately. `interim_status` can
explicitly declare `POSTED`, `PENDING`, or `REVIEW` for MT942 and non-final BAI
reports. The default is REVIEW. BAI modifier 2 is treated as final booked data;
other modifiers need review or a declared bank profile. Nonzero MT942 floor
limits produce a partial-report diagnostic.

## Identity and posting

A booked parent entry creates at most one posting. CAMT transaction details,
QIF splits, CODA details, and foreign-amount equivalents remain associated
source evidence; they are not additional journal entries. Opening/closing
balances are checked where the source profile provides a complete equation.
BAI file/group/account control totals and record counts, CODA totals/counts, and
Norma 43 totals/counts are checked independently of debit/credit presentation.
Balances never create plugs or complete reconciliation automatically.

New adapters expose transaction `status` and `identity`. POSTED is eligible for
materialization; PENDING and REVIEW are retained with the existing PENDING and
NEEDS_REVIEW source dispositions. These records reject journal classifications.
CODA duplicates and separate applications are retained for review. Summary
`retained_observations` counts new nonmaterialized observations;
`imported_transactions` counts new materialized statement movements.

Reliable native IDs preserve their source-account scope. Records without such
IDs receive deterministic IDs containing the original file hash and record
ordinal. Identical rows within one file remain separate; replaying the exact
file preserves identity. Changing the interpretation of an already imported
file fails with `IMPORT_REPLAY_CONFLICT` rather than reusing a stale batch.

A new file-scoped record, or a new ID from another format/account namespace,
requires review when existing activity in the mapped statement account has the
same booking date and signed amount. Description is not an identity test.
Inspect candidates before preview:

```sh
books --company example --json bank-import matches JOB --input choices.json
```

The HTTP equivalent is `POST /v1/companies/example/imports/JOB/matches` with the
same JSON choices; it needs only read permission and performs no writes. A
mapping can include:

```json
"identity_decisions": [{
  "transaction_id": "<source transaction ID>",
  "action": "duplicate",
  "existing_source_id": "<candidate source_record_id>",
  "reason": "Verified the same bank activity in an earlier export"
}]
```

`duplicate` retains a SOURCE_ONLY observation and creates no transaction or
journal. A booked movement can duplicate only a posted candidate; resolve
pending/review evidence explicitly first. Use `action: "new"` with a reason and
without `existing_source_id` for a verified separate movement. Classify only
booked, nonzero, nonduplicate movements when `post` is true. Decisions are bound
to the immutable preview, checked again during apply, and stale ledger state
invalidates the preview. Matching is a review aid based on date and amount,
not a guarantee that exports with different dates or amounts never overlap.

## Limits and standards references

Uploads are limited to 8 MiB; options to 32 KiB; choices to 2 MiB. Documents have
at most 100 accounts and 10,000 movements. Text/XML/ZIP/row/cell complexity is
bounded. XLSX permits at most 256 ZIP members, 32 MiB per expanded member,
64 MiB total expansion, and an expansion-ratio limit. Match inspection permits
at most 100 candidates per movement and 20,000 returned candidates per request.

The implementation uses original synthetic fixtures and these primary format
references; specifications themselves are not bundled:

- [Original Intuit QIF reference, preserved by W3C](https://www.w3.org/2000/10/swap/pim/qif-doc/QIF-doc.htm).
- [OFX downloads](https://www.ofx.net/downloads.html).
- [BAI Cash Management Balance Reporting v2](https://cdn.bai.org/migrated-from-www/docs/default-source/libraries/site-general-downloads/cash_management_2005.pdf).
- [X9.121 BTRS version 3](https://x9.org/wp-content/uploads/2018/07/X9.121-2016-BTRS-Version-3.0.pdf).
- [SWIFT CGI cash-management reporting guidance](https://www.swift.com/swift-resource/252088/download).
- [Rabobank MT940 Structured](https://media.rabobank.com/asset/2da2a993-ae7e-420b-b5eb-4f7355733902/Format-description-MT940S-Structured.pdf).
- [Febelfin CODA 2.7](https://febelfin.be/media/pages/publicaties/2023/febelfin-standaarden-voor-online-bankieren/5607daeda5-1754302976/standard-coda-2.7-en.pdf).
- [CFONB account statements](https://www.cfonb.org/fichiers/20130612113947_7_4_Releve_de_Compte_sur_support_informatique_2004_07.pdf).
- [Norma 43, June 2012](https://docs.bankinter.com/stf/plataformas/empresas/gestion/ficheros/formatos_fichero/norma_43_castellano.pdf).
- [SIX ISO 4217 currency and minor-unit definitions](https://www.six-group.com/en/products-services/financial-information/market-reference-data/data-standards.html).
