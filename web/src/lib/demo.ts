import type { Company, Snapshot } from "./books";
export const demoCompanies: Company[] = [
  { key: "maple", name: "Maple Household", currency: "USD", basis: "accrual" },
  { key: "studio", name: "Example Studio", currency: "USD", basis: "accrual" },
];
export function demoSnapshot(company: Company): Snapshot {
  const business = company.key === "studio";
  return {
    company,
    from: "2026-09-01",
    to: "2026-09-13",
    fetchedAt: "2026-09-13T14:30:00Z",
    demo: true,
    income: business ? "1840000" : "640000",
    expenses: business ? "1218000" : "284000",
    profit: business ? "622000" : "356000",
    accounts: [
      {
        id: "checking",
        code: "1000",
        name: business ? "Operating account" : "Everyday checking",
        kind: "BANK",
        opening: "121090",
        balance: "324000",
        movements: [
          {
            id: "repair",
            date: "2026-09-04",
            description: business ? "Office maintenance" : "Home maintenance",
            amount: "-48000",
            balance: "73090",
          },
          {
            id: "groceries",
            date: "2026-09-09",
            description: business ? "Office supplies" : "Market Square",
            amount: "-60000",
            balance: "13090",
          },
          {
            id: "pay",
            date: "2026-09-12",
            description: business ? "Client payment" : "Paycheck",
            amount: "320000",
            balance: "333090",
          },
          {
            id: "market",
            date: "2026-09-13",
            description: business ? "Team lunch" : "Market Square",
            amount: "-8420",
            balance: "324670",
          },
          {
            id: "coffee",
            date: "2026-09-13",
            description: "Coffee House",
            amount: "-670",
            balance: "324000",
          },
          {
            id: "fuel",
            date: "2026-09-13",
            description: "Fuel stop",
            amount: "-4200",
            balance: "324000",
            pending: true,
          },
        ],
      },
      {
        id: "savings",
        code: "1010",
        name: business ? "Tax reserve" : "Savings",
        kind: "BANK",
        opening: "518000",
        balance: "518000",
        movements: [],
      },
    ],
    points: [
      { date: "2026-09-01", amount: "639090" },
      { date: "2026-09-04", amount: "591090" },
      { date: "2026-09-09", amount: "531090" },
      { date: "2026-09-12", amount: "851090" },
      { date: "2026-09-13", amount: "842000" },
      { date: "2026-09-16", amount: "662000", projected: true },
      { date: "2026-09-20", amount: "982000", projected: true },
      { date: "2026-09-30", amount: "910000", projected: true },
    ],
  };
}
