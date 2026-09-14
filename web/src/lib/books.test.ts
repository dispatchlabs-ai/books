import { describe, expect, it } from "vitest";
import { money, fromLedger, sum, conversationContext } from "./books";
import { demoCompanies, demoSnapshot } from "./demo";
describe("exact money and ledger conversion", () => {
  it("preserves large minor units and zero/three decimal currencies", () => {
    expect(money("9007199254740993")).toBe("$90,071,992,547,409.93");
    expect(money("-1")).toBe("−$0.01");
    expect(money("12345", "COP")).toBe("COP123.45");
    expect(money("12345", "IQD")).toBe("IQD12.345");
    expect(money("12345", "CLF")).toBe("CLF1.2345");
    expect(money("1234", "JPY")).toBe("¥1,234");
    expect(money("1234", "KWD")).toBe("KWD1.234");
    expect(sum(["9007199254740993", "7"])).toBe("9007199254741000");
  });
  it("sums cash only, preserves liability signs and excludes revenue accounts", () => {
    const a = (
      code: string,
      subtype: string,
      opening: string,
      closing: string,
      change: string,
    ) => ({
      account: { id: code, code, name: code, subtype },
      opening_balance: { consolidated_cents: opening },
      closing_balance: { consolidated_cents: closing },
      lines: [
        {
          journal_id: code,
          line_number: 1,
          posting_date: "2026-09-10",
          journal_description: "Example",
          change_cents: change,
          running_balance_cents: closing,
        },
      ],
    });
    const report = fromLedger(
      demoCompanies[0],
      {
        accounts: [
          a("1000", "BANK", "100", "350", "250"),
          a("1010", "BANK", "20", "30", "10"),
          a("2100", "CREDIT_CARD", "-100", "-200", "-100"),
          a("4000", "OPERATING_REVENUE", "0", "-250", "-250"),
        ],
      },
      {
        total_revenue: { consolidated_cents: "250" },
        total_expenses: { consolidated_cents: "100" },
        net_income: { consolidated_cents: "150" },
      },
      "2026-09-01",
      "2026-09-13",
    );
    expect(report.accounts).toHaveLength(3);
    expect(report.accounts[2].balance).toBe("-200");
    expect(report.points.map((p) => p.amount)).toEqual(["120", "380", "380"]);
    expect(report.demo).toBe(false);
  });
  it("demo balances tie to settled movements and aggregate cash history", () => {
    for (const c of demoCompanies) {
      const s = demoSnapshot(c);
      for (const a of s.accounts)
        expect(
          sum([
            a.opening,
            ...a.movements.filter((m) => !m.pending).map((m) => m.amount),
          ]),
        ).toBe(a.balance);
      expect(s.points.filter((p) => !p.projected).at(-1)?.amount).toBe(
        sum(s.accounts.map((a) => a.balance)),
      );
    }
  });
});

it("bounds conversation bytes and keeps the latest question", () => {
  const context = conversationContext([
    { role: "user", content: "first" },
    { role: "assistant", content: "x".repeat(32000) },
    { role: "user", content: "latest" },
  ]);
  expect(context).toEqual([{ role: "user", content: "latest" }]);
});
