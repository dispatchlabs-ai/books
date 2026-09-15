import { describe, expect, it } from "vitest";
import {
  average,
  scenarioLabel,
  summarizeOutlook,
  verdict,
  type Forecast,
  type ForecastAccount,
  type ForecastEvent,
} from "./forecast";

const account = (
  code: string,
  name: string,
  extra: Partial<ForecastAccount> = {},
): ForecastAccount => ({
  code,
  name,
  kind: "bank",
  reserved: false,
  opening: "0",
  floor: "0",
  evidence: "Synthetic",
  ...extra,
});
let id = 0;
const event = (
  date: string,
  kind: ForecastEvent["kind"],
  accountCode: string,
  amount: number,
  extra: Partial<ForecastEvent> = {},
): ForecastEvent => ({
  id: `e${id++}`,
  name: "Synthetic",
  date,
  kind,
  account: accountCode,
  amount: String(amount),
  status: "estimated",
  evidence: "Synthetic",
  ...extra,
});
const forecast = (
  events: ForecastEvent[],
  days: Forecast["days"] = [],
  range = { as_of: "2026-09-14", through: "2026-11-30" },
): Forecast => ({
  digest: "synthetic",
  scenarios: ["baseline"],
  plan: {
    name: "Synthetic",
    currency: "USD",
    ...range,
    accounts: [
      account("1000", "Checking"),
      account("1010", "Medical Reserve", { reserved: true }),
      account("2100", "Card", { kind: "card" }),
    ],
    events,
    assumptions: [],
  },
  days,
  lows: [],
  warnings: [],
  variances: [],
});

describe("point-forward outlook", () => {
  it("excludes a partial first month from averages", () => {
    const o = summarizeOutlook(
      forecast([
        event("2026-09-15", "inflow", "1000", 500000),
        event("2026-10-15", "inflow", "1000", 500000),
        event("2026-10-20", "outflow", "1000", 480000),
        event("2026-11-15", "inflow", "1000", 500000),
        event("2026-11-20", "outflow", "1000", 510000),
      ]),
    );
    expect(o.months.map((m) => [m.month, m.from, m.to, m.complete])).toEqual([
      ["2026-09", "2026-09-15", "2026-09-30", false],
      ["2026-10", "2026-10-01", "2026-10-31", true],
      ["2026-11", "2026-11-01", "2026-11-30", true],
    ]);
    expect(o.months[0].leftOver).toBe(500000n);
    expect(o.averageLeftOver).toBe(5000n);
    expect(o.tightest?.month).toBe("2026-11");
    expect(o.shortMonths.map((m) => m.month)).toEqual(["2026-11"]);
  });

  it("counts card purchases once and never counts card payments or transfers as spending", () => {
    const o = summarizeOutlook(
      forecast([
        event("2026-10-02", "outflow", "2100", 12000, {
          category: "Groceries",
        }),
        event("2026-10-03", "outflow", "1000", 3000, { category: "Utilities" }),
        event("2026-10-24", "transfer", "1000", 12000, {
          to_account: "2100",
          arrival: "2026-10-24",
        }),
      ]),
    );
    const october = o.months[1];
    expect(october.spending).toBe(15000n);
    expect(october.toReserves).toBe(0n);
    expect(october.categories).toEqual([
      { name: "Groceries", amount: 12000n },
      { name: "Utilities", amount: 3000n },
    ]);
  });

  it("treats reserve contributions as allocations and nets reserve withdrawals and spending", () => {
    const o = summarizeOutlook(
      forecast([
        event("2026-10-15", "inflow", "1000", 100000),
        event("2026-10-16", "outflow", "1000", 90000),
        event("2026-10-16", "transfer", "1000", 30000, {
          to_account: "1010",
          arrival: "2026-10-16",
          status: "proposed",
        }),
        event("2026-10-20", "outflow", "1010", 4000),
        event("2026-10-25", "transfer", "1010", 1000, {
          to_account: "1000",
          arrival: "2026-10-27",
        }),
      ]),
    );
    const october = o.months[1];
    expect(october.income).toBe(100000n);
    expect(october.spending).toBe(94000n);
    expect(october.leftOver).toBe(6000n);
    expect(october.toReserves).toBe(25000n);
    expect(october.afterReserves).toBe(-19000n);
    expect(october.reserves).toEqual([
      { code: "1010", name: "Medical Reserve", amount: 25000n },
    ]);
    expect(o.reserveActivity).toBe(true);
  });

  it("suppresses replaced expectations and ignores activity already in the opening snapshot", () => {
    const expected = event("2026-10-03", "outflow", "1000", 20643, {
      id: "expected",
    });
    const o = summarizeOutlook(
      forecast([
        expected,
        event("2026-10-03", "outflow", "1000", 17950, {
          status: "actual",
          replaces: "expected",
        }),
        event("2026-09-12", "outflow", "1000", 14400, { status: "actual" }),
      ]),
    );
    expect(o.months[1].spending).toBe(17950n);
    expect(o.counts).toMatchObject({ total: 1, actual: 1, estimated: 0 });
  });

  it("reports each account's first shortfall, including the opening snapshot, lowest point and days below floor", () => {
    const day = (date: string, closing: number, shortfall: number) => ({
      date,
      account: "1000",
      opening: "0",
      inflow: "0",
      outflow: "0",
      closing: String(closing),
      floor: "0",
      shortfall: String(shortfall),
      movements: [],
    });
    const o = summarizeOutlook(
      forecast(
        [],
        [
          day("2026-09-14", -500, 500),
          day("2026-12-13", 100, 0),
          day("2026-12-14", -91260, 91260),
          day("2026-12-15", -1000, 1000),
        ],
        { as_of: "2026-09-14", through: "2026-12-31" },
      ),
    );
    expect(o.gaps).toEqual([
      {
        code: "1000",
        name: "Checking",
        reserved: false,
        floor: 0n,
        firstDate: "2026-09-14",
        lowestDate: "2026-12-14",
        lowest: -91260n,
        shortfall: 91260n,
        daysShort: 3,
      },
    ]);
  });

  it("rounds averages half away from zero in minor units", () => {
    expect(average([97939n, 5532n, 119795n])).toBe(74422n);
    expect(average([1n, 2n])).toBe(2n);
    expect(average([-1n, -2n])).toBe(-2n);
    expect(average([-85000n, -70000n, 96500n])).toBe(-19500n);
    expect(average([])).toBeUndefined();
  });

  it("words the answer from full months only", () => {
    const month = (date: string, income: number, spending: number) => [
      event(date, "inflow", "1000", income),
      event(date, "outflow", "1000", spending),
    ];
    const covered = summarizeOutlook(
      forecast([
        ...month("2026-09-20", 100, 900),
        ...month("2026-10-05", 900, 800),
        ...month("2026-11-05", 900, 900),
      ]),
    );
    expect(verdict(covered)).toEqual({
      tone: "covered",
      text: "Expected income covers planned spending in every full month.",
    });
    const oneShort = summarizeOutlook(
      forecast([
        ...month("2026-10-05", 900, 500),
        ...month("2026-11-05", 900, 901),
      ]),
    );
    expect(verdict(oneShort).text).toBe(
      "Planned spending is more than expected income in November.",
    );
    const averageShort = summarizeOutlook(
      forecast([
        ...month("2026-10-05", 900, 1000),
        ...month("2026-11-05", 900, 850),
      ]),
    );
    expect(verdict(averageShort).text).toBe(
      "Planned spending is more than expected income on average.",
    );
    const partialOnly = summarizeOutlook(
      forecast([], [], { as_of: "2026-09-14", through: "2026-10-20" }),
    );
    expect(verdict(partialOnly)).toEqual({
      tone: "none",
      text: "This plan doesn’t cover a full month yet.",
    });
    expect(scenarioLabel("funded_with-savings")).toBe("Funded with savings");
  });
});
