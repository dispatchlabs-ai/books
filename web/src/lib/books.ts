import { currencyScales } from "./currencies";
export type Company = {
  key: string;
  name: string;
  currency: string;
  basis: string;
};
export type Movement = {
  id: string;
  date: string;
  description: string;
  amount: string;
  balance: string;
  pending?: boolean;
};
export type Account = {
  id: string;
  code: string;
  name: string;
  kind: string;
  balance: string;
  opening: string;
  movements: Movement[];
};
export type Point = { date: string; amount: string; projected?: boolean };
export type Snapshot = {
  company: Company;
  from: string;
  to: string;
  fetchedAt: string;
  demo: boolean;
  accounts: Account[];
  income: string;
  expenses: string;
  profit: string;
  points: Point[];
};
export type Answer = {
  text: string;
  sources?: { label: string; account: string }[];
};
export function scale(currency: string): number {
  const value = currencyScales[currency];
  if (value === undefined)
    throw new Error("Unsupported Books currency: " + currency);
  return value;
}
// Accounting and formatted values never pass through floating point.
export function money(value: string | bigint, currency = "USD"): string {
  const n = BigInt(value),
    digits = scale(currency),
    unit = 10n ** BigInt(digits);
  const abs = n < 0n ? -n : n;
  const whole = new Intl.NumberFormat("en-US").format(abs / unit);
  const fraction = digits
    ? "." + (abs % unit).toString().padStart(digits, "0")
    : "";
  const symbol =
    new Intl.NumberFormat("en-US", { style: "currency", currency })
      .formatToParts(0)
      .find((p) => p.type === "currency")?.value ?? currency;
  return `${n < 0n ? "−" : ""}${symbol}${whole}${fraction}`;
}
export function signedMoney(value: bigint, currency = "USD"): string {
  return (value > 0n ? "+" : "") + money(value, currency);
}
export function sum(values: string[]): string {
  return values.reduce((a, b) => a + BigInt(b), 0n).toString();
}
export function dateLabel(date: string): string {
  return new Date(date + "T12:00:00").toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}
export function dayLabel(date: string): string {
  return new Date(date + "T12:00:00").toLocaleDateString("en-US", {
    weekday: "short",
    month: "short",
    day: "numeric",
  });
}
// "Sep 15–30", "Oct 1–Dec 31", or with years when a range crosses one.
export function rangeLabel(from: string, to: string): string {
  if (from === to) return dateLabel(from);
  if (from.slice(0, 4) !== to.slice(0, 4))
    return `${dateLabel(from)}, ${from.slice(0, 4)}–${dateLabel(to)}, ${to.slice(0, 4)}`;
  return from.slice(0, 7) === to.slice(0, 7)
    ? `${dateLabel(from)}–${Number(to.slice(8))}`
    : `${dateLabel(from)}–${dateLabel(to)}`;
}
export function today(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
export async function request<T>(
  path: string,
  signal?: AbortSignal,
  body?: unknown,
): Promise<T> {
  const response = await fetch("/api" + path, {
    signal,
    credentials: "same-origin",
    headers: body ? { "Content-Type": "application/json" } : {},
    ...(body ? { method: "POST", body: JSON.stringify(body) } : {}),
  });
  const data = await response.json();
  if (!response.ok)
    throw new Error(
      data.error ?? "Books could not load this view. Please try again.",
    );
  return data as T;
}
type Breakdown = { consolidated_cents: string };
type Ledger = {
  accounts: {
    account: { id: string; code: string; name: string; subtype: string };
    opening_balance: Breakdown;
    closing_balance: Breakdown;
    lines: {
      journal_id: string;
      line_number: number;
      posting_date: string;
      journal_description: string;
      change_cents: string;
      running_balance_cents: string;
    }[];
  }[];
};
type Performance = {
  total_revenue: Breakdown;
  total_expenses: Breakdown;
  net_income: Breakdown;
};
export function fromLedger(
  company: Company,
  ledger: Ledger,
  performance: Performance,
  from: string,
  to: string,
): Snapshot {
  const accounts = (ledger.accounts ?? [])
    .filter((a) =>
      ["BANK", "CREDIT_CARD", "INVESTMENT", "LOAN"].includes(a.account.subtype),
    )
    .map((a) => ({
      id: a.account.id,
      code: a.account.code,
      name: a.account.name,
      kind: a.account.subtype,
      balance: a.closing_balance.consolidated_cents,
      opening: a.opening_balance.consolidated_cents,
      movements: (a.lines ?? []).map((l) => ({
        id: `${l.journal_id}:${l.line_number}`,
        date: l.posting_date,
        description: l.journal_description,
        amount: l.change_cents,
        balance: l.running_balance_cents,
      })),
    }));
  const cash = accounts.filter((a) => a.kind === "BANK");
  let balance = BigInt(sum(cash.map((a) => a.opening)));
  const daily = new Map<string, bigint>();
  for (const m of cash.flatMap((a) => a.movements))
    daily.set(m.date, (daily.get(m.date) ?? 0n) + BigInt(m.amount));
  const points: Point[] = [{ date: from, amount: balance.toString() }];
  for (const [date, change] of [...daily].sort(([a], [b]) =>
    a.localeCompare(b),
  )) {
    balance += change;
    points.push({ date, amount: balance.toString() });
  }
  if (points.at(-1)?.date !== to)
    points.push({ date: to, amount: balance.toString() });
  return {
    company,
    from,
    to,
    fetchedAt: new Date().toISOString(),
    demo: false,
    accounts,
    income: performance.total_revenue.consolidated_cents,
    expenses: performance.total_expenses.consolidated_cents,
    profit: performance.net_income.consolidated_cents,
    points: [...new Map(points.map((p) => [p.date, p])).values()],
  };
}
export async function loadSnapshot(
  company: Company,
  signal: AbortSignal,
  range?: { from: string; to: string },
): Promise<Snapshot> {
  const to = range?.to ?? today(),
    from = range?.from ?? to.slice(0, 7) + "-01",
    scope = `/books/companies/${encodeURIComponent(company.key)}`;
  const [ledger, performance] = await Promise.all([
    request<Ledger>(
      `${scope}/reports/general-ledger?from=${from}&to=${to}&include_zero=true`,
      signal,
    ),
    request<Performance>(
      `${scope}/reports/profit-loss?from=${from}&to=${to}`,
      signal,
    ),
  ]);
  return fromLedger(company, ledger, performance, from, to);
}

export function conversationContext(
  messages: { role: string; content: string }[],
  byteLimit = 24000,
) {
  const result: { role: string; content: string }[] = [];
  let bytes = 2;
  for (const m of messages.toReversed()) {
    const size = new TextEncoder().encode(JSON.stringify(m)).length + 1;
    if (bytes + size > byteLimit || result.length >= 20) break;
    result.unshift(m);
    bytes += size;
  }
  while (result[0]?.role === "assistant") result.shift();
  return result;
}
