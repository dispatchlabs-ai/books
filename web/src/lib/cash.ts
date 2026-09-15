import type { Forecast } from "./forecast";
export type Horizon = "week" | "month" | "year";
export const horizons: Horizon[] = ["week", "month", "year"];
export function cashWindow(
  forecast: Forecast,
  horizon: Horizon,
  today: string,
) {
  const date = new Date(today + "T00:00:00Z");
  date.setUTCDate(
    date.getUTCDate() + { week: 7, month: 30, year: 365 }[horizon] - 1,
  );
  const through = date.toISOString().slice(0, 10);
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
