// Point-forward interpretation of a books.cash-plan/v1 forecast result. Every
// amount is an integer minor-unit bigint; nothing here uses floating point.
export type ForecastEvent = {
  id: string;
  name: string;
  date: string;
  kind: "inflow" | "outflow" | "transfer";
  arrival?: string;
  account: string;
  to_account?: string;
  amount: string;
  status: "confirmed" | "estimated" | "proposed" | "actual";
  evidence: string;
  category?: string;
  replaces?: string;
};
export type ForecastAccount = {
  code: string;
  name: string;
  kind: "bank" | "card";
  reserved: boolean;
  opening: string;
  floor: string;
  evidence: string;
};
export type ForecastDay = {
  date: string;
  account: string;
  opening: string;
  inflow: string;
  outflow: string;
  closing: string;
  floor: string;
  shortfall: string;
  movements: { event: ForecastEvent; delta: string; balance: string }[];
};
export type Forecast = {
  digest: string;
  scenarios: string[];
  plan: {
    name: string;
    as_of: string;
    through: string;
    currency: string;
    accounts: ForecastAccount[];
    events?: ForecastEvent[];
    assumptions?: string[];
  };
  days: ForecastDay[];
  lows: { account: string; date: string; balance: string; shortfall: string }[];
  warnings: string[];
  variances: {
    expected: ForecastEvent;
    actual: ForecastEvent;
    amount_difference: string;
  }[];
};

export type MonthOutlook = {
  month: string;
  from: string;
  to: string;
  complete: boolean;
  income: bigint;
  spending: bigint;
  leftOver: bigint;
  toReserves: bigint;
  afterReserves: bigint;
  reserves: { code: string; name: string; amount: bigint }[];
  categories: { name: string; amount: bigint }[];
};
export type AccountGap = {
  code: string;
  name: string;
  reserved: boolean;
  floor: bigint;
  firstDate: string;
  lowestDate: string;
  lowest: bigint;
  shortfall: bigint;
  daysShort: number;
};
export type Outlook = {
  asOf: string;
  through: string;
  months: MonthOutlook[];
  complete: MonthOutlook[];
  averageLeftOver?: bigint;
  averageToReserves?: bigint;
  averageAfterReserves?: bigint;
  tightest?: MonthOutlook;
  shortMonths: MonthOutlook[];
  reserveActivity: boolean;
  gaps: AccountGap[];
  counts: Record<ForecastEvent["status"], number> & { total: number };
  accounts: { bank: number; reserved: number; card: number };
};

function lastDay(month: string) {
  const [y, m] = month.split("-").map(Number);
  return `${month}-${String(new Date(Date.UTC(y, m, 0)).getUTCDate()).padStart(2, "0")}`;
}
function later(a: string, b: string) {
  return a > b ? a : b;
}
function earlier(a: string, b: string) {
  return a < b ? a : b;
}
// Rounds half away from zero to the nearest minor unit.
export function average(values: bigint[]): bigint | undefined {
  if (!values.length) return undefined;
  const total = values.reduce((a, b) => a + b, 0n),
    n = BigInt(values.length),
    q = total / n,
    r = total % n;
  const abs = r < 0n ? -r : r;
  return abs * 2n >= n ? q + (total < 0n ? -1n : 1n) : q;
}

/**
 * Groups the scenario's future events by the calendar month money moves.
 * Income is every inflow. Spending is every outflow, including purchases on
 * cards; card payments and transfers are never counted as spending again.
 * Net to reserves is the change in reserved bank balances: transfers in, less
 * transfers out and spending paid from a reserve. Left over less net to
 * reserves is therefore the change in non-reserved money. A month is complete
 * only when the plan covers its first and last day; partial months are shown
 * but excluded from averages.
 */
export function summarizeOutlook(forecast: Forecast): Outlook {
  const { plan } = forecast;
  const accounts = new Map(plan.accounts.map((a) => [a.code, a]));
  const replaced = new Set(
    (plan.events ?? []).flatMap((e) => (e.replaces ? [e.replaces] : [])),
  );
  const horizon = (plan.events ?? []).filter(
    (e) => !replaced.has(e.id) && e.date > plan.as_of && e.date <= plan.through,
  );
  const months = new Map<string, MonthOutlook>();
  const start = new Date(plan.as_of + "T00:00:00Z");
  start.setUTCDate(start.getUTCDate() + 1);
  for (
    let cursor = start.toISOString().slice(0, 7);
    cursor <= plan.through.slice(0, 7);
  ) {
    const from = later(`${cursor}-01`, start.toISOString().slice(0, 10)),
      to = earlier(lastDay(cursor), plan.through);
    months.set(cursor, {
      month: cursor,
      from,
      to,
      complete: from === `${cursor}-01` && to === lastDay(cursor),
      income: 0n,
      spending: 0n,
      leftOver: 0n,
      toReserves: 0n,
      afterReserves: 0n,
      reserves: [],
      categories: [],
    });
    const [y, m] = cursor.split("-").map(Number);
    cursor =
      m === 12 ? `${y + 1}-01` : `${y}-${String(m + 1).padStart(2, "0")}`;
  }
  const reserveTotals = new Map<string, Map<string, bigint>>();
  const categoryTotals = new Map<string, Map<string, bigint>>();
  const bump = (
    store: Map<string, Map<string, bigint>>,
    month: string,
    key: string,
    amount: bigint,
  ) => {
    const inner = store.get(month) ?? new Map<string, bigint>();
    inner.set(key, (inner.get(key) ?? 0n) + amount);
    store.set(month, inner);
  };
  const counts = {
    total: horizon.length,
    confirmed: 0,
    estimated: 0,
    proposed: 0,
    actual: 0,
  };
  for (const e of horizon) {
    counts[e.status]++;
    const month = months.get(e.date.slice(0, 7));
    const source = accounts.get(e.account);
    if (!month || !source) continue;
    const amount = BigInt(e.amount);
    const reserved = (a?: ForecastAccount) => a?.kind === "bank" && a.reserved;
    if (e.kind === "inflow") {
      month.income += amount;
      if (reserved(source)) {
        month.toReserves += amount;
        bump(reserveTotals, month.month, source.code, amount);
      }
    } else if (e.kind === "outflow") {
      month.spending += amount;
      bump(categoryTotals, month.month, e.category || "Uncategorized", amount);
      if (reserved(source)) {
        month.toReserves -= amount;
        bump(reserveTotals, month.month, source.code, -amount);
      }
    } else {
      const destination = accounts.get(e.to_account ?? "");
      if (reserved(source) === reserved(destination)) continue;
      const signed = reserved(destination) ? amount : -amount;
      const code = reserved(destination) ? destination!.code : source.code;
      month.toReserves += signed;
      bump(reserveTotals, month.month, code, signed);
    }
  }
  const list = [...months.values()].map((m) => {
    m.leftOver = m.income - m.spending;
    m.afterReserves = m.leftOver - m.toReserves;
    m.reserves = [...(reserveTotals.get(m.month) ?? new Map())]
      .filter(([, amount]) => amount !== 0n)
      .map(([code, amount]) => ({
        code,
        name: accounts.get(code)?.name ?? code,
        amount,
      }));
    m.categories = [...(categoryTotals.get(m.month) ?? new Map())]
      .map(([name, amount]) => ({ name, amount }))
      .sort((a, b) => (b.amount > a.amount ? 1 : b.amount < a.amount ? -1 : 0));
    return m;
  });
  const complete = list.filter((m) => m.complete);
  const gaps: AccountGap[] = [];
  for (const account of plan.accounts) {
    if (account.kind !== "bank") continue;
    const short = forecast.days.filter(
      (d) => d.account === account.code && BigInt(d.shortfall) > 0n,
    );
    if (!short.length) continue;
    const low = short.reduce((a, b) =>
      BigInt(b.closing) < BigInt(a.closing) ? b : a,
    );
    gaps.push({
      code: account.code,
      name: account.name,
      reserved: account.reserved,
      floor: BigInt(account.floor),
      firstDate: short[0].date,
      lowestDate: low.date,
      lowest: BigInt(low.closing),
      shortfall: BigInt(low.shortfall),
      daysShort: short.length,
    });
  }
  gaps.sort(
    (a, b) =>
      a.firstDate.localeCompare(b.firstDate) || a.name.localeCompare(b.name),
  );
  return {
    asOf: plan.as_of,
    through: plan.through,
    months: list,
    complete,
    averageLeftOver: average(complete.map((m) => m.leftOver)),
    averageToReserves: average(complete.map((m) => m.toReserves)),
    averageAfterReserves: average(complete.map((m) => m.afterReserves)),
    tightest: complete.reduce<MonthOutlook | undefined>(
      (a, m) => (!a || m.leftOver < a.leftOver ? m : a),
      undefined,
    ),
    shortMonths: complete.filter((m) => m.leftOver < 0n),
    reserveActivity: list.some((m) => m.reserves.length > 0),
    gaps,
    counts,
    accounts: {
      bank: plan.accounts.filter((a) => a.kind === "bank").length,
      reserved: plan.accounts.filter((a) => a.kind === "bank" && a.reserved)
        .length,
      card: plan.accounts.filter((a) => a.kind === "card").length,
    },
  };
}

export function monthName(month: string, style: "long" | "short" = "long") {
  return new Date(month + "-01T12:00:00Z").toLocaleDateString("en-US", {
    month: style,
    timeZone: "UTC",
  });
}
export function daysBetween(from: string, to: string) {
  return Math.round(
    (Date.parse(to + "T00:00:00Z") - Date.parse(from + "T00:00:00Z")) /
      86400000,
  );
}

export function scenarioLabel(key: string) {
  const words = key.replace(/[-_]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}
export const monthSpan = (months: MonthOutlook[]) =>
  months.length === 1
    ? monthName(months[0].month, "short")
    : `${monthName(months[0].month, "short")}–${monthName(months.at(-1)!.month, "short")}`;
const list = (names: string[]) =>
  names.length < 3
    ? names.join(" and ")
    : `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;

// The one-sentence answer shared by the Outlook page and the overview card.
export function verdict(o: Outlook): {
  tone: "covered" | "short" | "none";
  text: string;
} {
  if (!o.complete.length)
    return { tone: "none", text: "This plan doesn’t cover a full month yet." };
  if (o.averageLeftOver! < 0n)
    return {
      tone: "short",
      text: "Planned spending is more than expected income on average.",
    };
  if (o.shortMonths.length)
    return {
      tone: "short",
      text: `Planned spending is more than expected income in ${list(o.shortMonths.map((m) => monthName(m.month)))}.`,
    };
  return {
    tone: "covered",
    text:
      o.complete.length === 1
        ? "Expected income covers planned spending this full month."
        : "Expected income covers planned spending in every full month.",
  };
}
