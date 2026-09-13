// Monetary *_cents fields are integer strings in the owning currency: do not assume /100.
// Minimal transport example, not an offline store or generated complete SDK.
export type Grant = "read" | "import" | "post" | "manage";
export type StatementFormat = "OFX" | "QFX" | "QBO" | "QIF" | "CSV" | "TSV" | "XLSX" | "CAMT" | "MT940" | "MT942" | "BAI2" | "BTRS" | "CODA" | "CFONB120" | "NORMA43";
export type SourceStatus = "POSTED" | "PENDING" | "REVIEW";
export interface ImportOptions {
  format?: StatementFormat;
  institution?: string;
  account_id?: string;
  account_kind?: "BANK" | "CREDIT_CARD";
  currency?: string;
  date_layout?: string;
  year_pivot?: number;
  encoding?: "UTF-8" | "WINDOWS-1252" | "ISO-8859-1" | "CP850";
  mt_booking_date?: "statement" | "value";
  interim_status?: SourceStatus;
  tabular?: {
    header_row: number;
    date_column: string;
    description_columns: string[];
    decimal_separator: "." | ",";
    sheet?: string;
    delimiter?: string;
    id_column?: string;
    value_date_column?: string;
    amount_column?: string;
    debit_column?: string;
    credit_column?: string;
    account_column?: string;
    currency_column?: string;
    status_column?: string;
    status_values?: Record<string, SourceStatus>;
    thousands_separator?: string;
    invert_sign?: boolean;
  };
}
export interface SourceDetail {
  id?: string;
  amount?: string;
  currency?: string;
  description?: string;
  fields?: Record<string, string>;
}
export interface ImportMatches {
  job_id: string;
  ledger_revision: string;
  matches: Array<{
    account_key: string;
    transaction_id: string;
    posted_date: string;
    amount: string;
    candidates: Array<{
      source_record_id: string;
      source_system: string;
      external_id: string;
      description: string;
      disposition: string;
    }>;
  }>;
}
export interface ImportChoices {
  post: boolean;
  mappings: Array<{
    account_key: string;
    statement_account: string;
    identity_decisions?: Array<{
      transaction_id: string;
      action: "new" | "duplicate";
      existing_source_id?: string;
      reason: string;
    }>;
    classifications?: Array<{ transaction_id: string; contra_account: string }>;
  }>;
  exclusions?: Array<{ account_key: string; reason: string }>;
}
export interface ImportPlan {
  id: string;
  job_id: string;
  digest: string;
  ledger_revision: string;
  choices: ImportChoices;
  summary: ImportSummary;
  created_at: string;
}
export interface ImportSummary {
  retained_observations?: number;
  imported_transactions: number;
  skipped_transactions: number;
  new_journals: number;
  existing_journals: number;
}
export interface ImportReceipt {
  job_id: string;
  plan_id: string;
  plan_digest: string;
  summary: ImportSummary;
  accounts: Array<{ statement_account: string; batch_id: string }>;
  journal_ids: string[];
  applied_at: string;
}
export interface ImportJob {
  id: string;
  status: "UPLOADED" | "READY" | "FAILED" | "APPLIED";
  options?: ImportOptions;
  error?: { code: string; message: string };
  document?: {
    format: StatementFormat;
    version: string;
    parser: string;
    accounts: Array<{
      key: string;
      kind: string;
      account_id: string;
      currency: string;
      transactions: Array<{
        id: string;
        amount: string;
        posted_date: string;
        description: string;
        identity?: "NATIVE" | "FILE";
        status?: SourceStatus;
        details?: SourceDetail[];
      }>;
    }>;
  };
  receipt?: ImportReceipt;
}
export interface TransactionRequest {
  amount: string;
  date: string;
  account?: string;
  from?: string;
  to?: string;
  description?: string;
  reference?: string;
  draft?: boolean;
  dry_run?: boolean;
}
export interface JournalInput {
  posting_date: string;
  description: string;
  period?: string;
  reference?: string;
  lines: Array<{account: string; debit?: string; credit?: string; description?: string}>;
}
export interface Transaction {
  number?: string;
  book: string;
  company: string;
  date: string;
  status: string;
  total_debit_cents: string;
  total_credit_cents: string;
  dry_run: boolean;
}
export interface Correction {
  original_number: string;
  reversal: Transaction;
  replacement: Transaction;
  reason: string;
}
export interface ReconciliationRequest {
  statement_account: string;
  through: string;
  ending: string;
  start?: string;
  beginning?: string;
  clear_all?: boolean;
  cleared?: string[];
  target_reconciliation_id?: string;
}
// Preserve the entire returned plan, including nulls and string integers.
// Clients display it for review and return it unchanged; they never recompute it.
export type JSONValue = string | number | boolean | null | JSONValue[] | {[key: string]: JSONValue};
export interface ReviewPlan {
  schema: string;
  company: string;
  book: string;
  digest: string;
  [key: string]: JSONValue;
}
export interface AccountInput {
  kind: "bank" | "ar" | "asset" | "fixed-asset" | "investment" | "ap" | "credit-card" | "loan" | "liability" | "equity" | "income" | "expense";
  name: string;
  code: string;
  active_from: string;
  reconcile_from?: string;
  no_reconcile?: boolean;
  default_payment?: boolean;
  default_deposit?: boolean;
  retained_earnings?: boolean;
  dry_run?: boolean;
}
export interface AccountDefaults {
  payment_account: string;
  deposit_account: string;
  retained_earnings: string;
}
export class BooksError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
export class BooksClient {
  private baseURL: string;
  private token: () => Promise<string>;
  private company: string;
  constructor(baseURL: string, token: () => Promise<string>, company: string) {
    this.baseURL = baseURL;
    this.token = token;
    this.company = company;
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers = new Headers(options.headers);
    headers.set("Authorization", `Bearer ${await this.token()}`);
    const response = await fetch(`${this.baseURL.replace(/\/$/, "")}/v1/companies/${encodeURIComponent(this.company)}${path}`, {
      ...options, headers, credentials: "omit", redirect: "error",
    });
    const body = await response.json();
    if (body.schema !== "books.api/v1") throw new BooksError(response.status, "PROTOCOL_INVALID", "Unsupported Books response");
    if (!response.ok || !body.ok) throw new BooksError(response.status, body.error?.code ?? "REQUEST_FAILED", body.error?.message ?? "Books request failed");
    return body.data as T;
  }
  private workflow<T>(path: string, body: unknown, stableKey?: string): Promise<T> {
    const headers: Record<string, string> = {"Content-Type": "application/json"};
    if (stableKey !== undefined) headers["Idempotency-Key"] = stableKey;
    return this.request(path, {method: "POST", body: JSON.stringify(body), headers});
  }
  record(kind: "spend" | "receive" | "transfer", input: TransactionRequest, stableKey: string): Promise<Transaction> {
    return this.workflow(`/transactions/${kind}`, input, stableKey);
  }
  journal(journal: JournalInput, stableKey: string, draft = false): Promise<Transaction> {
    return this.workflow("/journals", {journal, draft}, stableKey);
  }
  changeStatus(number: string, action: "post" | "abandon", dryRun = false): Promise<Transaction> {
    return this.workflow(`/transactions/${encodeURIComponent(number)}/${action}`, {dry_run: dryRun});
  }
  reverse(number: string, date: string, description: string, draft = false): Promise<Transaction> {
    return this.workflow(`/transactions/${encodeURIComponent(number)}/reverse`, {date, description, draft});
  }
  correct(number: string, journal: JournalInput, reason: string, draft = false): Promise<Correction> {
    return this.workflow(`/transactions/${encodeURIComponent(number)}/correct`, {journal, reason, draft});
  }
  planReconciliation(input: ReconciliationRequest): Promise<ReviewPlan> {
    return this.workflow("/reconciliations/plan", input);
  }
  planClose(period: string): Promise<ReviewPlan> {
    return this.workflow("/close/plan", {period});
  }
  planYearClose(fiscalYear: number, retainedEarnings?: string): Promise<ReviewPlan> {
    return this.workflow("/year-close/plan", {fiscal_year: fiscalYear, retained_earnings: retainedEarnings});
  }
  applyReviewedPlan(kind: "reconciliations" | "close" | "year-close", plan: ReviewPlan, dryRun = false): Promise<Record<string, JSONValue>> {
    return this.workflow(`/${kind}/apply`, {plan, dry_run: dryRun});
  }
  createAccount(input: AccountInput): Promise<Record<string, JSONValue>> {
    return this.workflow("/accounts", input);
  }
  defaults(): Promise<AccountDefaults> { return this.request("/defaults"); }
  setDefault(key: "payment-account" | "deposit-account" | "retained-earnings", account: string): Promise<AccountDefaults> {
    return this.workflow("/defaults", {key, account});
  }
  configureYear(fiscalYear: number): Promise<Record<string, JSONValue>> {
    return this.workflow("/periods", {fiscal_year: fiscalYear});
  }
  reopen(kind: "periods" | "reconciliations", id: string, reason: string): Promise<Record<string, JSONValue>> {
    return this.workflow(`/${kind}/${encodeURIComponent(id)}/reopen`, {reason});
  }
  upload(bytes: Uint8Array<ArrayBuffer>, name: string, stableKey: string): Promise<ImportJob> {
    return this.request(`/imports?name=${encodeURIComponent(name)}`, {
      method: "POST", body: bytes,
      headers: { "Content-Type": "application/octet-stream", "Idempotency-Key": stableKey },
    });
  }
  uploadWithOptions(bytes: Uint8Array<ArrayBuffer>, name: string, options: ImportOptions, stableKey: string): Promise<ImportJob> {
    const body = new FormData();
    body.append("file", new Blob([bytes]), name);
    body.append("options", JSON.stringify(options));
    // Fetch supplies the multipart boundary; do not set Content-Type manually.
    return this.request("/imports", {
      method: "POST", body, headers: { "Idempotency-Key": stableKey },
    });
  }
  matches(id: string, choices: ImportChoices): Promise<ImportMatches> {
    return this.request(`/imports/${encodeURIComponent(id)}/matches`, {
      method: "POST", body: JSON.stringify(choices),
      headers: { "Content-Type": "application/json" },
    });
  }
  job(id: string): Promise<ImportJob> {
    return this.request(`/imports/${encodeURIComponent(id)}`);
  }
  preview(id: string, choices: ImportChoices, stableKey: string): Promise<ImportPlan> {
    return this.request(`/imports/${encodeURIComponent(id)}/previews`, {
      method: "POST", body: JSON.stringify(choices),
      headers: { "Content-Type": "application/json", "Idempotency-Key": stableKey },
    });
  }
  // Call only after displaying/reviewing the plan. Persist its ID and digest so
  // a lost response can be recovered by retrying the exact same request.
  apply(plan: Pick<ImportPlan, "id" | "digest">): Promise<ImportReceipt> {
    return this.request(`/import-plans/${encodeURIComponent(plan.id)}/apply`, {
      method: "POST", headers: { "If-Match": `"${plan.digest}"` },
    });
  }
}
