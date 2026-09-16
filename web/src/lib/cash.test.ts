import { describe, it, expect } from "vitest";
import { cashWindow, expectedIncome } from "./cash";
import type { Forecast, ForecastEvent } from "./forecast";
const make = (balances: string[], kind = "bank") =>
  ({
    plan: {
      as_of: "2026-09-14",
      through: "2026-09-21",
      accounts: [{ code: "a", name: "Bills", kind, opening: "500" }],
    },
    days: balances.map((closing, i) => ({
      account: "a",
      date: `2026-09-${15 + i}`,
      closing,
    })),
  }) as Forecast;
describe("cash horizons", () => {
  it("finds a dip even when ending positive, using exact amounts", () => {
    const r = cashWindow(
      make(["9007199254740993", "-1", "300", "300", "300", "300", "300"]),
      "week",
      "2026-09-15",
    ).rows[0];
    expect(r.first).toBe("2026-09-16");
    expect(r.low).toBe(-1n);
    expect(r.complete).toBe(true);
  });
  it("excludes negative days outside the horizon and card liabilities", () => {
    expect(
      cashWindow(
        make(["1", "1", "1", "1", "1", "1", "1", "-900"]),
        "week",
        "2026-09-15",
      ).rows[0].first,
    ).toBeUndefined();
    expect(
      cashWindow(make(["-100"], "card"), "week", "2026-09-15").rows,
    ).toEqual([]);
  });
  it("never marks a truncated year or a missing day complete", () => {
    const f = make(["5", "5", "5", "5", "5", "5", "5"]);
    expect(cashWindow(f, "year", "2026-09-15").rows[0].complete).toBe(false);
    f.days.splice(2, 1);
    expect(cashWindow(f, "week", "2026-09-15").rows[0].complete).toBe(false);
  });
  it("includes opening snapshot on the start date, not a prior date", () => {
    const f = make(["-5"]);
    f.plan.as_of = "2026-09-15";
    f.days = [];
    f.plan.accounts[0].opening = "-20";
    expect(cashWindow(f, "week", "2026-09-15").rows[0].low).toBe(-20n);
    expect(cashWindow(f, "week", "2026-09-16").rows[0].low).toBeUndefined();
  });
});

describe("expected income", () => {
  const receipt = (
    id: string,
    date: string,
    amount = "100",
    extra: Partial<ForecastEvent> = {},
  ): ForecastEvent => ({
    id,
    name: id,
    date,
    amount,
    kind: "inflow",
    account: "a",
    status: "estimated",
    evidence: "Synthetic payment schedule",
    ...extra,
  });
  const plan = (events: ForecastEvent[]) => {
    const forecast = make([]);
    forecast.plan.through = "2027-09-30";
    forecast.plan.accounts.push(
      {
        code: "reserve",
        name: "Reserve",
        kind: "bank",
        reserved: true,
        opening: "0",
        floor: "0",
        evidence: "Synthetic",
      },
      {
        code: "card",
        name: "Card",
        kind: "card",
        reserved: false,
        opening: "0",
        floor: "0",
        evidence: "Synthetic",
      },
    );
    forecast.plan.events = events;
    return forecast;
  };
  it("counts only dated external bank receipts with exact totals, including earmarked income", () => {
    const forecast = plan([
      receipt("last", "2026-09-21", "9007199254740993"),
      receipt("first", "2026-09-15", "101", { account: "reserve" }),
      receipt("before", "2026-09-14"),
      receipt("after", "2026-09-22"),
      receipt("transfer", "2026-09-16", "500", {
        kind: "transfer",
        to_account: "reserve",
        arrival: "2026-09-17",
      }),
      receipt("card refund", "2026-09-16", "500", { account: "card" }),
      receipt("spending", "2026-09-16", "500", { kind: "outflow" }),
    ]);
    const result = expectedIncome(forecast, "week", "2026-09-15")!;
    expect(result.total).toBe(9007199254741094n);
    expect(result.receipts.map((r) => [r.event.id, r.account.name])).toEqual([
      ["first", "Reserve"],
      ["last", "Bills"],
    ]);
    expect(result.complete).toBe(true);
    expect(expectedIncome(forecast, "month", "2026-09-15")!.total).toBe(
      result.total + 100n,
    );
  });
  it("suppresses replaced estimates even when the actual is outside the window or in opening cash", () => {
    const forecast = plan([
      receipt("estimate", "2026-09-17", "500"),
      receipt("actual", "2026-09-20", "475", {
        status: "actual",
        replaces: "estimate",
      }),
    ]);
    expect(expectedIncome(forecast, "week", "2026-09-15")!.total).toBe(475n);
    forecast.plan.events![1].date = "2026-09-23";
    expect(expectedIncome(forecast, "week", "2026-09-15")!.total).toBe(0n);
    expect(expectedIncome(forecast, "month", "2026-09-15")!.total).toBe(475n);
    forecast.plan.events![1].date = "2026-09-14";
    expect(expectedIncome(forecast, "month", "2026-09-15")!.total).toBe(0n);
  });
  it("reports partial coverage instead of extrapolating a year or counting opening-day receipts", () => {
    const forecast = plan([
      receipt("opening", "2026-09-15", "500"),
      receipt("future", "2026-09-16", "250"),
    ]);
    forecast.plan.as_of = "2026-09-15";
    forecast.plan.through = "2026-09-20";
    expect(expectedIncome(forecast, "year", "2026-09-15")).toMatchObject({
      total: 250n,
      from: "2026-09-16",
      through: "2026-09-20",
      complete: false,
    });
    expect(expectedIncome(forecast, "week", "2026-09-21")).toBeUndefined();
    expect(expectedIncome(forecast, "week", "2026-09-01")).toBeUndefined();
  });
  it("shows zero for a covered window with no scheduled receipts", () => {
    const forecast = plan([]);
    delete forecast.plan.events;
    expect(expectedIncome(forecast, "week", "2026-09-15")).toMatchObject({
      total: 0n,
      complete: true,
      receipts: [],
    });
  });
});
