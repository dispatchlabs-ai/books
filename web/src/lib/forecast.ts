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
  /** The plan covers the month's first and last day. */
  complete: boolean;
  /** Any income or spending is planned in the month. */
  planned: boolean;
  /** The month ended before today; its figures are old estimates. */
  ended: boolean;
  /** Why the month is left out of averages and the answer, if it is. */
  excluded?: "ended" | "partial" | "unplanned";
  income: bigint;
  spending: bigint;
  leftOver: bigint;
  toReserves: bigint;
  afterReserves: bigint;
  /** Net increase in money between departure and arrival. */
  inTransit: bigint;
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
  /** Full, planned months that haven't ended: the basis of the answer. */
  counted: MonthOutlook[];
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
 * Groups the scenario's future events by the calendar month money leaves (or
 * arrives, for income). Income is every inflow to a bank account. Spending is
 * every outflow, including purchases on cards, less credits on cards; card
 * payments and transfers are never spending. Reserve balance changes use each
 * transfer leg's actual date, including transfers between reserves. Subtracting
 * reserve changes and the change in transit from surplus gives the change in
 * other bank and card balances, even across month boundaries.
 *
 * Only full, planned months that haven't ended before `today` count toward the
 * answer and averages; the rest stay visible with the reason they're excluded.
 */
export function summarizeOutlook(forecast: Forecast, today = ""): Outlook {
  const { plan } = forecast;
  const accounts = new Map(plan.accounts.map((a) => [a.code, a]));
  const replaced = new Set(
    (plan.events ?? []).flatMap((e) => (e.replaces ? [e.replaces] : [])),
  );
  const horizon = (plan.events ?? []).filter(
    (e) =>
      !replaced.has(e.id) &&
      ((e.date > plan.as_of && e.date <= plan.through) ||
        (e.kind === "transfer" &&
          e.arrival! > plan.as_of &&
          e.arrival! <= plan.through)),
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
      planned: false,
      ended: Boolean(today) && to < today,
      income: 0n,
      spending: 0n,
      leftOver: 0n,
      toReserves: 0n,
      afterReserves: 0n,
      inTransit: 0n,
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
  const reserved = (a?: ForecastAccount) => a?.kind === "bank" && a.reserved;
  const reserveLeg = (
    month: MonthOutlook,
    a: ForecastAccount,
    amount: bigint,
  ) => {
    if (!reserved(a)) return;
    month.toReserves += amount;
    bump(reserveTotals, month.month, a.code, amount);
  };
  for (const e of horizon) {
    counts[e.status]++;
    const month =
      e.date > plan.as_of && e.date <= plan.through
        ? months.get(e.date.slice(0, 7))
        : undefined;
    const source = accounts.get(e.account);
    if (!source) continue;
    const amount = BigInt(e.amount);
    if (e.kind === "transfer") {
      if (month) {
        month.inTransit += amount;
        reserveLeg(month, source, -amount);
      }
      const arrival =
        e.arrival! > plan.as_of && e.arrival! <= plan.through
          ? months.get(e.arrival!.slice(0, 7))
          : undefined;
      const destination = accounts.get(e.to_account ?? "");
      if (arrival && destination) {
        arrival.inTransit -= amount;
        reserveLeg(arrival, destination, amount);
      }
      continue;
    }
    if (!month) continue;
    month.planned = true;
    if (e.kind === "inflow" && source.kind === "card") {
      // A refund or credit on a card offsets spending; it isn't income.
      month.spending -= amount;
      bump(categoryTotals, month.month, e.category || "Card credits", -amount);
    } else if (e.kind === "inflow") {
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
    }
  }
  const list = [...months.values()].map((m) => {
    m.leftOver = m.income - m.spending;
    m.excluded = m.ended
      ? "ended"
      : !m.complete
        ? "partial"
        : !m.planned
          ? "unplanned"
          : undefined;
    m.afterReserves = m.leftOver - m.toReserves - m.inTransit;
    m.reserves = [...(reserveTotals.get(m.month) ?? new Map())].map(
      ([code, amount]) => ({
        code,
        name: accounts.get(code)?.name ?? code,
        amount,
      }),
    );
    m.categories = [...(categoryTotals.get(m.month) ?? new Map())]
      .map(([name, amount]) => ({ name, amount }))
      .sort((a, b) => (b.amount > a.amount ? 1 : b.amount < a.amount ? -1 : 0));
    return m;
  });
  const counted = list.filter((m) => !m.excluded);
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
    counted,
    averageLeftOver: average(counted.map((m) => m.leftOver)),
    averageToReserves: average(counted.map((m) => m.toReserves)),
    averageAfterReserves: average(counted.map((m) => m.afterReserves)),
    tightest: counted.reduce<MonthOutlook | undefined>(
      (a, m) => (!a || m.leftOver < a.leftOver ? m : a),
      undefined,
    ),
    shortMonths: counted.filter((m) => m.leftOver < 0n),
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
// Spending as a share of income, to one decimal place, without floating point.
export function spentShare(m: MonthOutlook): string | undefined {
  if (m.income <= 0n) return undefined;
  if (m.spending < 0n) return undefined;
  const tenths = (m.spending * 1000n + m.income / 2n) / m.income;
  return `${tenths / 10n}.${tenths % 10n}%`;
}
const list = (names: string[]) =>
  names.length < 3
    ? names.join(" and ")
    : `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
const nextMonth = (month: string) => {
  const [y, m] = month.split("-").map(Number);
  return m === 12 ? `${y + 1}-01` : `${y}-${String(m + 1).padStart(2, "0")}`;
};
// "Oct–Dec" for consecutive months; otherwise each month is named.
export function monthSpan(months: MonthOutlook[]) {
  if (!months.length) return "";
  const names = months.map((m) => monthName(m.month, "short"));
  const consecutive = months.every(
    (m, i) => i === 0 || nextMonth(months[i - 1].month) === m.month,
  );
  return consecutive && months.length > 1
    ? `${names[0]}–${names.at(-1)}`
    : list(names);
}
// The one-sentence answer shared by the Outlook page and the overview card.
export function verdict(o: Outlook): {
  tone: "covered" | "short" | "none";
  text: string;
} {
  if (!o.counted.length) {
    const full = o.months.filter((m) => m.complete);
    return {
      tone: "none",
      text: full.some((m) => m.ended)
        ? "The full months in this plan have already ended. Update the plan to see what’s ahead."
        : full.length
          ? "This plan has no income or spending in a full month."
          : "This plan doesn’t cover a full month yet.",
    };
  }
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
      o.counted.length === 1
        ? "Expected income covers planned spending in its one full month."
        : "Expected income covers planned spending in every full month.",
  };
}
