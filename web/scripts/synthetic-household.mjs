// Synthetic household cash plans for previews and integration tests. Every name,
// balance and event is invented; the shape mirrors a real point-forward plan:
// a partial first month, three complete months, a thin month, card payoffs,
// constrained proposed reserve funding and a remaining account gap.
const cents = (dollars) => String(Math.round(dollars * 100));

export const householdAccounts = [
  { code: "1000", name: "Income", kind: "bank", opening: 2150, floor: 0 },
  {
    code: "1010",
    name: "Fixed Bills",
    kind: "bank",
    opening: 3480,
    floor: 250,
  },
  {
    code: "1020",
    name: "Household Spending",
    kind: "bank",
    opening: 1120,
    floor: 0,
  },
  {
    code: "1030",
    name: "Kids Activities",
    kind: "bank",
    opening: 310,
    floor: 0,
  },
  {
    code: "1040",
    name: "Card Payments",
    kind: "bank",
    opening: 1900,
    floor: 0,
  },
  {
    code: "1050",
    name: "Home + Auto Reserve",
    kind: "bank",
    opening: 1850,
    floor: 1850,
    reserved: true,
  },
  {
    code: "1060",
    name: "Medical Reserve",
    kind: "bank",
    opening: 420,
    floor: 420,
    reserved: true,
  },
  {
    code: "1070",
    name: "Emergency Savings",
    kind: "bank",
    opening: 6200,
    floor: 0,
    reserved: true,
  },
  {
    code: "2100",
    name: "Everyday Card",
    kind: "card",
    opening: -1640,
    floor: 0,
  },
];

const asOf = "2026-09-14";
const through = "2026-12-31";
const paydays = [
  "2026-09-15",
  "2026-09-30",
  "2026-10-15",
  "2026-10-30",
  "2026-11-13",
  "2026-11-30",
  "2026-12-15",
  "2026-12-31",
];
const months = ["2026-09", "2026-10", "2026-11", "2026-12"];

function days(month) {
  const [y, m] = month.split("-").map(Number);
  const count = new Date(Date.UTC(y, m, 0)).getUTCDate();
  return Array.from(
    { length: count },
    (_, i) => `${month}-${String(i + 1).padStart(2, "0")}`,
  );
}
// Allocates a monthly allowance across every day in exact cents.
function daily(month, total) {
  const all = days(month),
    each = Math.floor(total / all.length);
  return all.map((d, i) => [
    d,
    i === all.length - 1 ? total - each * (all.length - 1) : each,
  ]);
}

export function syntheticHousehold() {
  const events = [];
  const add = (e) => {
    if (e.date > asOf && e.date <= through)
      events.push({ status: "estimated", evidence: "Synthetic example", ...e });
  };
  for (const date of paydays)
    add({
      id: `salary:${date}`,
      date,
      name: "Net salary",
      kind: "inflow",
      account: "1000",
      amount: cents(8400),
      category: "Salary",
    });
  for (const month of months) {
    const bill = (
      day,
      name,
      amount,
      category = "Fixed bills",
      account = "1010",
    ) =>
      add({
        id: `${name}:${month}`.toLowerCase().replaceAll(" ", "-"),
        date: `${month}-${day}`,
        name,
        kind: "outflow",
        account,
        amount: cents(amount),
        category,
      });
    bill("01", "Mortgage", 4850);
    bill("05", "Health insurance", 1920);
    bill("12", "Utilities", 610);
    bill("18", "Internet and phone", 330);
    if (month !== "2026-12") bill("20", "Auto lease", 540);
    bill("22", "Auto insurance", 470);
    bill("25", "Life insurance", 150);
    bill("01", "Activity tuition", 420, "Kids activities", "1030");
    bill(
      "09",
      "Streaming services",
      64,
      "Fixed bills / digital services",
      "2100",
    );
    bill("21", "Cloud storage", 118, "Fixed bills / digital services", "2100");
    bill(
      "16",
      "Software subscription",
      95,
      "Business expense paid personally — reimbursement timing unknown",
      "2100",
    );
    for (const [date, amount] of daily(month, 310000))
      add({
        id: `household:${date}`,
        date,
        name: "Household spending allowance",
        kind: "outflow",
        account: "1020",
        amount: String(amount),
        category: "Variable allowance",
      });
    for (const [date, amount] of daily(month, 24000))
      add({
        id: `transport:${date}`,
        date,
        name: "Fuel and tolls allowance",
        kind: "outflow",
        account: "2100",
        amount: String(amount),
        category: "Transport allowance",
      });
    for (const [date, amount] of daily(month, 184800))
      add({
        id: `shopping:${date}`,
        date,
        name: "Dining and shopping allowance",
        kind: "outflow",
        account: "2100",
        amount: String(amount),
        category: "Variable allowance",
      });
    // December's dated competition estimate replaces that month's daily kids allowance.
    if (month !== "2026-12")
      for (const [date, amount] of daily(month, 58000))
        add({
          id: `kids:${date}`,
          date,
          name: "Kids activities allowance",
          kind: "outflow",
          account: "1030",
          amount: String(amount),
          category: "Kids activities",
        });
  }
  for (const date of [
    "2026-09-28",
    "2026-10-12",
    "2026-10-26",
    "2026-11-09",
    "2026-11-23",
    "2026-12-07",
    "2026-12-21",
  ])
    add({
      id: `cleaning:${date}`,
      date,
      name: "House cleaning",
      kind: "outflow",
      account: "1010",
      amount: cents(195),
      category: "Fixed bills",
    });
  add({
    id: "kids:school-photos",
    date: "2026-10-07",
    name: "School photos",
    kind: "outflow",
    account: "1030",
    amount: cents(145),
    category: "Kids activities",
  });
  add({
    id: "renewal:home-warranty",
    date: "2026-11-03",
    name: "Home warranty renewal",
    kind: "outflow",
    account: "2100",
    amount: cents(640),
    category: "Renewal estimate",
    evidence:
      "Synthetic example: prior-year notice; current amount unconfirmed",
  });
  add({
    id: "renewal:hoa",
    date: "2026-11-20",
    name: "HOA annual dues",
    kind: "outflow",
    account: "1010",
    amount: cents(385),
    category: "Renewal estimate",
    evidence: "Synthetic example: renewal amount unconfirmed",
  });
  add({
    id: "kids:competition",
    date: "2026-12-14",
    name: "Team competition travel",
    kind: "outflow",
    account: "1030",
    amount: cents(1300),
    category: "Kids activities",
  });
  for (const [date, amount] of [
    ["2026-09-24", 1640],
    ["2026-10-24", 1020],
    ["2026-11-24", 1680],
    ["2026-12-24", 1020],
  ])
    add({
      id: `card-payment:${date}`,
      date,
      name: "Everyday Card payment",
      kind: "transfer",
      account: "1040",
      to_account: "2100",
      arrival: date,
      amount: cents(amount),
      category: "Card payment — liability settlement",
    });

  const accounts = householdAccounts.map((a) => ({
    code: a.code,
    name: a.name,
    kind: a.kind,
    opening: cents(a.opening),
    floor: cents(a.floor),
    reserved: Boolean(a.reserved),
    evidence: "Synthetic end-of-day balance, September 14",
  }));
  const assumptions = [
    "Synthetic example household. Opening balances are end-of-day snapshots on September 14; no live refresh is implied.",
    "Net salary $8,400 on the 15th and last business day.",
    "Household spending $3,100, kids activities $580 and transport $240 monthly, allocated across days in exact cents.",
    "Card purchases increase the card balance; only card payments reduce bank cash.",
    "Home warranty and HOA renewals are estimates pending current notices.",
    "Reserve accounts stay visible and separate from working cash. Their opening balances are protected floors.",
  ];
  const plan = (name, extra = [], notes = []) => ({
    version: "books.cash-plan/v1",
    name,
    currency: "USD",
    as_of: asOf,
    through,
    accounts,
    events: [...events, ...extra],
    assumptions: [...assumptions, ...notes],
  });
  const funding = [];
  const fund = (date, to, amount, name) =>
    funding.push({
      id: `fund:${to}:${date}`,
      date,
      name,
      kind: "transfer",
      account: "1000",
      to_account: to,
      arrival: date,
      amount: cents(amount),
      status: "proposed",
      category: "Account funding — not expense",
      evidence: "Synthetic proposed transfer",
    });
  paydays.forEach((date, i) => {
    fund(date, "1010", 4700, "Fund Fixed Bills");
    fund(date, "1020", 1550, "Fund Household Spending");
    fund(date, "1030", 490, "Fund Kids Activities");
    fund(date, "1040", 680, "Fund card payments");
    // Full reserve contributions fit only in the first four paydays.
    if (i < 4 || i % 2 === 0)
      fund(date, "1050", 450, "Home + Auto Reserve contribution");
    if (i < 4) fund(date, "1060", 440, "Medical Reserve contribution");
    else if (i === 4)
      fund(date, "1060", 300, "Medical Reserve contribution to target");
  });
  return {
    baseline: plan("Maple Household — baseline estimates"),
    funded: plan(
      "Maple Household — proposed funding with remaining gaps",
      funding,
      [
        "Funded adds proposed transfers after each payday. Home + Auto receives $450 and Medical up to $440 per payday while cash allows; Medical stops at its $2,480 target. No transfer is executed.",
      ],
    ),
  };
}
