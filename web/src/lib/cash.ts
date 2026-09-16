import type { Forecast } from "./forecast";
export type Horizon = "week" | "month" | "year";
export const horizons: Horizon[] = ["week", "month", "year"];
export function horizonEnd(horizon: Horizon, today: string) {
  const date = new Date(today + "T00:00:00Z");
  date.setUTCDate(
    date.getUTCDate() + { week: 7, month: 30, year: 365 }[horizon] - 1,
  );
  return date.toISOString().slice(0, 10);
}

/** External bank receipts in this window, never transfers or card credits.
 * Use the effective plan events so actual replacements suppress estimates even
 * when the replacement falls outside the window or into the opening snapshot.
 */
export function expectedIncome(
  forecast: Forecast,
  horizon: Horizon,
  today: string,
) {
  const { plan } = forecast;
  const requestedThrough = horizonEnd(horizon, today);
  const next = new Date(plan.as_of + "T00:00:00Z");
  next.setUTCDate(next.getUTCDate() + 1);
  const from = [today, next.toISOString().slice(0, 10)].sort().at(-1)!;
  const through = [requestedThrough, plan.through].sort()[0];
  if (from > through) return undefined;
  const banks = new Map(
    plan.accounts.filter((a) => a.kind === "bank").map((a) => [a.code, a]),
  );
  const events = plan.events ?? [];
  const replaced = new Set(
    events.flatMap((e) => (e.replaces ? [e.replaces] : [])),
  );
  const receipts = events
    .filter(
      (e) =>
        e.kind === "inflow" &&
        banks.has(e.account) &&
        !replaced.has(e.id) &&
        e.date >= from &&
        e.date <= through,
    )
    .map((event) => ({ event, account: banks.get(event.account)! }))
    .sort(
      (a, b) =>
        a.event.date.localeCompare(b.event.date) ||
        a.event.id.localeCompare(b.event.id),
    );
  return {
    from,
    through,
    complete: from === today && through === requestedThrough,
    total: receipts.reduce((sum, r) => sum + BigInt(r.event.amount), 0n),
    receipts,
  };
}
export function cashWindow(
  forecast: Forecast,
  horizon: Horizon,
  today: string,
) {
  const through = horizonEnd(horizon, today);
  const rows = forecast.plan.accounts
    .filter((a) => a.kind === "bank")
    .map((account) => {
      const days = forecast.days
        .filter(
          (d) =>
            d.account === account.code && d.date >= today && d.date <= through,
        )
        .sort((a, b) => a.date.localeCompare(b.date));
      const points = days.map((d) => ({
        date: d.date,
        value: BigInt(d.closing),
      }));
      if (forecast.plan.as_of === today)
        points.unshift({ date: today, value: BigInt(account.opening) });
      const unique = [...new Map(points.map((p) => [p.date, p])).values()];
      const complete =
        unique.length === { week: 7, month: 30, year: 365 }[horizon] &&
        unique[0]?.date === today &&
        unique.at(-1)?.date === through;
      const first = unique.find((p) => p.value < 0n);
      const low = unique.reduce<bigint | undefined>(
        (min, p) => (min === undefined || p.value < min ? p.value : min),
        undefined,
      );
      return { account, points: unique, complete, first: first?.date, low };
    })
    .sort(
      (a, b) =>
        (a.first ?? "9999").localeCompare(b.first ?? "9999") ||
        a.account.name.localeCompare(b.account.name),
    );
  return { rows, through };
}
