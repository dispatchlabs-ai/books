import { describe, it, expect } from "vitest";
import { cashWindow } from "./cash";
import type { Forecast } from "./forecast";
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
