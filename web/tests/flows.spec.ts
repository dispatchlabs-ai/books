import { test, expect } from "@playwright/test";
test("connection errors stay errors and never switch to fictional balances", async ({
  page,
}) => {
  await page.route("**/api/config", (route) =>
    route.fulfill({ json: { demo: false, agent: false } }),
  );
  await page.route("**/api/books/companies", (route) =>
    route.fulfill({ status: 503, json: { error: "Connection unavailable" } }),
  );
  await page.goto("/");
  await expect(page.getByRole("alert")).toHaveText("Connection unavailable");
  await expect(page.getByText("$8,420.00", { exact: true })).not.toBeVisible();
});

type PlanEvent = {
  id: string;
  date: string;
  name: string;
  kind: "inflow" | "outflow" | "transfer";
  account: string;
  to_account?: string;
  arrival?: string;
  amount: string;
  status: "confirmed" | "estimated" | "proposed" | "actual";
  evidence: string;
  category?: string;
};
// A tiny end-of-day projector for synthetic browser fixtures. The real engine's
// arithmetic is covered by the Go tests and the web integration test.
function project(name: string, events: PlanEvent[]) {
  const accounts = [
    {
      code: "1000",
      name: "Checking",
      kind: "bank",
      reserved: false,
      opening: "150000",
      floor: "20000",
      evidence: "Synthetic bank snapshot",
    },
    {
      code: "1030",
      name: "Kids Activities",
      kind: "bank",
      reserved: false,
      opening: "30000",
      floor: "0",
      evidence: "Synthetic kids snapshot",
    },
    {
      code: "1050",
      name: "Home Reserve",
      kind: "bank",
      reserved: true,
      opening: "100000",
      floor: "0",
      evidence: "Synthetic reserve snapshot",
    },
    {
      code: "2100",
      name: "Everyday Card",
      kind: "card",
      reserved: false,
      opening: "0",
      floor: "0",
      evidence: "Synthetic card snapshot",
    },
  ];
  const plan = {
    name,
    as_of: "2026-09-29",
    through: "2026-11-30",
    currency: "USD",
    accounts,
    events,
    assumptions: ["Synthetic browser fixture."],
  };
  const balance = new Map(accounts.map((a) => [a.code, BigInt(a.opening)]));
  const days = [];
  for (
    let t = Date.parse("2026-09-29T00:00:00Z");
    t <= Date.parse("2026-11-30T00:00:00Z");
    t += 86400000
  ) {
    const date = new Date(t).toISOString().slice(0, 10);
    for (const a of accounts) {
      const opening = balance.get(a.code)!;
      const legs = events.flatMap((e) => [
        ...(e.date === date && e.date > plan.as_of && e.account === a.code
          ? [{ e, delta: -BigInt(e.amount) * (e.kind === "inflow" ? -1n : 1n) }]
          : []),
        ...(e.kind === "transfer" &&
        e.arrival === date &&
        e.to_account === a.code
          ? [{ e, delta: BigInt(e.amount) }]
          : []),
      ]);
      let running = opening,
        inflow = 0n,
        outflow = 0n;
      const movements = legs.map(({ e, delta }) => {
        running += delta;
        if (delta > 0n) inflow += delta;
        else outflow -= delta;
        return { event: e, delta: String(delta), balance: String(running) };
      });
      balance.set(a.code, running);
      const shortfall =
        a.kind === "bank" && running < BigInt(a.floor)
          ? BigInt(a.floor) - running
          : 0n;
      days.push({
        date,
        account: a.code,
        opening: String(opening),
        inflow: String(inflow),
        outflow: String(outflow),
        closing: String(running),
        floor: a.floor,
        shortfall: String(shortfall),
        movements,
      });
    }
  }
  return {
    digest: `synthetic-${name}`,
    scenarios: ["baseline", "funded"],
    plan,
    days,
    lows: [],
    warnings: [],
    variances: [],
  };
}
const e = (
  id: string,
  date: string,
  kind: PlanEvent["kind"],
  account: string,
  amount: number,
  extra: Partial<PlanEvent> = {},
): PlanEvent => ({
  id,
  date,
  name: id,
  kind,
  account,
  amount: String(amount),
  status: "estimated",
  evidence: `Synthetic evidence for ${id}`,
  ...extra,
});
const baselineEvents = [
  e("Salary Sep 30", "2026-09-30", "inflow", "1000", 500000, {
    category: "Salary",
  }),
  e("Salary Oct 15", "2026-10-15", "inflow", "1000", 500000, {
    category: "Salary",
  }),
  e("Salary Nov 15", "2026-11-15", "inflow", "1000", 500000, {
    category: "Salary",
  }),
  e("Rent Oct", "2026-10-01", "outflow", "1000", 300000, {
    category: "Housing",
  }),
  e("Rent Nov", "2026-11-01", "outflow", "1000", 300000, {
    category: "Housing",
  }),
  e("Groceries Oct", "2026-10-10", "outflow", "2100", 120000, {
    category: "Groceries",
  }),
  e("Groceries Nov", "2026-11-10", "outflow", "2100", 180000, {
    category: "Groceries",
  }),
  e("Card payment Oct", "2026-10-24", "transfer", "1000", 120000, {
    to_account: "2100",
    arrival: "2026-10-24",
    category: "Card payment",
  }),
  e("Team registration", "2026-11-20", "outflow", "1030", 45000, {
    category: "Kids activities",
  }),
];
const funded = [
  ...baselineEvents,
  e("Home Reserve Sep", "2026-09-30", "transfer", "1000", 20000, {
    to_account: "1050",
    arrival: "2026-09-30",
    status: "proposed",
  }),
  e("Home Reserve Oct", "2026-10-15", "transfer", "1000", 60000, {
    to_account: "1050",
    arrival: "2026-10-15",
    status: "proposed",
  }),
  e("Home Reserve Nov", "2026-11-15", "transfer", "1000", 30000, {
    to_account: "1050",
    arrival: "2026-11-15",
    status: "proposed",
  }),
];
const householdForecast = {
  baseline: project("Example — baseline", baselineEvents),
  funded: project("Example — proposed funding", funded),
};

async function connectedHousehold(
  page: import("@playwright/test").Page,
  {
    failFunded = false,
    today = "2026-09-30",
    holdFunded,
  }: {
    failFunded?: boolean;
    today?: string;
    holdFunded?: Promise<void>;
  } = {},
) {
  // Month inclusion depends on today's date, so the browser clock is fixed.
  await page.clock.setFixedTime(new Date(`${today}T12:00:00`));
  const company = {
    key: "example",
    name: "Example Household",
    currency: "USD",
    basis: "ACCRUAL",
  };
  await page.route("**/api/config", (r) =>
    r.fulfill({
      json: {
        demo: false,
        agent: false,
        forecasts: { example: ["baseline", "funded"] },
      },
    }),
  );
  await page.route("**/api/books/companies", (r) =>
    r.fulfill({ json: [company] }),
  );
  await page.route("**/api/books/companies/example/reports/**", (r) =>
    r.fulfill({
      json: r.request().url().includes("general-ledger")
        ? {
            accounts: [
              ...[
                { id: "legacy", code: "1000", name: "Checking", subtype: "" },
                {
                  id: "extra",
                  code: "1099",
                  name: "Extra bank",
                  subtype: "BANK",
                },
              ].map((account) => ({
                account,
                opening_balance: { consolidated_cents: "77700" },
                closing_balance: { consolidated_cents: "77700" },
                lines: [],
              })),
              {
                account: {
                  id: "kids",
                  code: "1030",
                  name: "Kids Activities",
                  subtype: "BANK",
                },
                opening_balance: { consolidated_cents: "30000" },
                closing_balance: { consolidated_cents: "30000" },
                lines: [],
              },
              {
                account: {
                  id: "card",
                  code: "2100",
                  name: "Everyday Card",
                  subtype: "CREDIT_CARD",
                },
                opening_balance: { consolidated_cents: "0" },
                closing_balance: { consolidated_cents: "0" },
                lines: [],
              },
            ],
          }
        : {
            total_revenue: { consolidated_cents: "0" },
            total_expenses: { consolidated_cents: "0" },
            net_income: { consolidated_cents: "0" },
          },
    }),
  );
  let failures = failFunded ? 2 : 0;
  await page.route(
    "**/api/books/companies/example/cash-forecast?**",
    async (r) => {
      const scenario = new URL(r.request().url()).searchParams.get(
        "scenario",
      ) as "baseline" | "funded";
      if (scenario === "funded" && holdFunded) await holdFunded;
      if (scenario === "funded" && failures > 0) {
        failures--;
        return r.fulfill({
          status: 404,
          json: { error: "Scenario unavailable" },
        });
      }
      return r.fulfill({ json: householdForecast[scenario] });
    },
  );
}
const noPageScroll = (page: import("@playwright/test").Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

test("Cash is the default with bounded horizons and bank-only rows", async ({
  page,
}) => {
  await connectedHousehold(page);
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Cash", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("tab", { name: "Month", exact: true }),
  ).toHaveAttribute("data-state", "active");
  await expect(
    page
      .getByText("Lowest balance", { exact: true })
      .filter({ visible: true })
      .first(),
  ).toBeVisible();
  await expect(page.getByTestId("cash-row-1000")).toContainText("$777.00");
  await page.getByRole("tab", { name: "Year", exact: true }).click();
  await expect(
    page.getByText(/Forecast incomplete for this period/),
  ).toBeVisible();
  await expect(
    page.getByText("Stays at or above zero", { exact: true }),
  ).toHaveCount(0);
  await page.getByRole("tab", { name: "Week", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "Week", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  expect(await noPageScroll(page)).toBe(true);
  await page.screenshot({
    path: "/tmp/books-cash-" + page.viewportSize()!.width + ".png",
    fullPage: true,
  });
  await expect(page.getByTestId("cash-row-1099")).toContainText("No forecast");
  await expect(
    page.getByRole("button", { name: /^(Accounts|Outlook|Ask Books)$/ }),
  ).toHaveCount(0);
  await expect(page.getByTestId("cash-row-2100")).toHaveCount(0);
});

test("Cash shows a failed ledger load and can retry", async ({ page }) => {
  await connectedHousehold(page);
  await page.route("**/api/books/companies/example/reports/**", (r) =>
    r.fulfill({ status: 503, json: { error: "Ledger offline" } }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("alert").filter({ hasText: "Bank balances unavailable" }),
  ).toContainText("Ledger offline");
  await expect(
    page.getByRole("button", { name: "Retry balances" }),
  ).toBeVisible();
  await expect(page.getByText("Loading bank accounts…")).toHaveCount(0);
});

test("Cash compares two-month spending and saves explicit monthly budgets", async ({
  page,
}) => {
  await connectedHousehold(page);
  let revision = "one",
    monthly: string | null = "20000";
  let saves = 0;
  const budgetData = () => ({
    can_edit: true,
    from: "2026-07-01",
    to: "2026-08-31",
    months: ["2026-07", "2026-08"],
    rows: [
      {
        account: "1000",
        months: ["12000", "18000"],
        average: "15000",
        monthly,
        count: 2,
      },
    ],
    unassigned: {
      account: "",
      months: ["0", "5000"],
      average: "2500",
      monthly: null,
      count: 1,
    },
    expenses: [],
    plan: {
      revision,
      buckets: [{ account: "1000", monthly, expense_accounts: ["5000"] }],
      assignments: [],
    },
    accounts: [
      { code: "1000", name: "Checking", type: "ASSET", subtype: "BANK" },
      { code: "5000", name: "Groceries", type: "EXPENSE", subtype: "" },
    ],
  });
  await page.route("**/api/books/companies/example/budget", (r) =>
    r.fulfill({ json: budgetData() }),
  );
  await page.route("**/api/books/companies/example/budget/save", (r) => {
    saves++;
    const input = r.request().postDataJSON();
    expect(input.plan.revision).toBe(revision);
    monthly = input.plan.buckets.find(
      (b: { account: string }) => b.account === "1000",
    ).monthly;
    revision = "two";
    return r.fulfill({ json: budgetData() });
  });
  await page.goto("/");
  const row = page.getByTestId("cash-row-1000");
  await expect(row).toContainText("$150.00");
  await expect(row).toContainText("$200.00");
  await expect(page.getByText(/\$25.00 \/ month unassigned/)).toBeVisible();
  await page.getByRole("button", { name: "Edit budgets" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /^Select entity/ }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Refresh view" }),
  ).toBeDisabled();
  for (const width of [320, 390, 760, 761, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await expect.poll(() => noPageScroll(page)).toBe(true);
  }
  const amount = row.getByRole("textbox", {
    name: "Monthly budget for Checking",
    exact: true,
  });
  await amount.fill("-1");
  await page.getByRole("button", { name: "Save budgets" }).click();
  await expect(amount).toHaveAttribute("aria-invalid", "true");
  await expect(amount).toBeFocused();
  expect(saves).toBe(0);
  await page
    .getByRole("textbox", { name: "Monthly budget for Checking", exact: true })
    .fill("175.50");
  await page.getByRole("button", { name: "Save budgets" }).click();
  await expect(amount).toHaveCount(0);
  await expect(row).toContainText("$175.50");
  await page.getByRole("tab", { name: "Week", exact: true }).click();
  await expect(row).toContainText("$150.00");
  await page.getByRole("button", { name: "Edit budgets" }).click();
  await amount.fill("999");
  await page.getByRole("tab", { name: "Month", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(
    page.getByRole("tab", { name: "Year", exact: true }),
  ).toBeFocused();
  await expect(amount).toHaveValue("999");
  await page.keyboard.press("ArrowLeft");
  await expect(
    page.getByRole("tab", { name: "Month", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Edit budgets" }),
  ).toBeFocused();
  await expect(row).toContainText("$175.50");
  expect(saves).toBe(1);
  for (const [value, expected] of [
    ["0", "$0.00"],
    ["", "Not set"],
  ]) {
    await page.getByRole("button", { name: "Edit budgets" }).click();
    await amount.fill(value);
    await page.getByRole("button", { name: "Save budgets" }).click();
    await expect(row).toContainText(expected);
    await expect(amount).toHaveCount(0);
  }
  expect(await noPageScroll(page)).toBe(true);
  await page.screenshot({
    path: "/tmp/books-budgets-" + page.viewportSize()!.width + ".png",
    fullPage: true,
  });
});

test("entity selection stays scoped and narrow layouts fit", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByTestId("cash-row-1000")).toContainText(
    "Everyday checking",
  );
  for (const width of [320, 390, 760, 761, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await expect
      .poll(() => noPageScroll(page), { message: `Layout at ${width}px` })
      .toBe(true);
  }
  await page
    .getByRole("button", { name: "Select entity, current: Maple Household" })
    .click();
  await page.getByRole("menuitem", { name: "Example Studio" }).click();
  await expect(
    page.getByRole("button", {
      name: "Select entity, current: Example Studio",
    }),
  ).toBeVisible();
  await expect(page.getByTestId("cash-row-1000")).toContainText(
    "Operating account",
  );
  await expect(
    page.getByText("Everyday checking", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText(
      "No forecast for this entity. Future shortfalls are unknown.",
    ),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Month", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(
    page.getByRole("tab", { name: "Year", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await page.reload();
  await expect(
    page.getByRole("button", {
      name: "Select entity, current: Example Studio",
    }),
  ).toBeVisible();
  await expect(page.getByTestId("cash-row-1000")).toContainText(
    "Operating account",
  );
});

test("failed forecasts keep posted banks visible and recover", async ({
  page,
}) => {
  await connectedHousehold(page);
  let fails = true;
  await page.route("**/api/books/companies/example/cash-forecast?**", (r) =>
    fails
      ? r.fulfill({ status: 503, json: { error: "Forecast offline" } })
      : r.fulfill({ json: householdForecast.baseline }),
  );
  await page.route("**/api/books/companies/example/budget", (r) =>
    r.fulfill({ status: 503, json: { error: "Budgets offline" } }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("alert").filter({ hasText: "Forecast unavailable" }),
  ).toBeVisible();
  await expect(page.getByTestId("cash-row-1099")).toContainText("$777.00");
  await expect(page.getByTestId("cash-row-1099")).toContainText("Unavailable");
  await expect(page.getByTestId("cash-row-1099")).not.toContainText("Not set");
  fails = false;
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "Forecast unavailable" }),
  ).toHaveCount(0);
  await expect(page.getByTestId("cash-row-1000")).toBeVisible();
});

test("budget editor fits 320px and preserves conflicts, categories and purchase overrides", async ({
  page,
}) => {
  await connectedHousehold(page);
  await page.setViewportSize({ width: 320, height: 900 });
  const budget = {
    can_edit: true,
    months: ["2026-07", "2026-08"],
    from: "2026-07-01",
    to: "2026-08-31",
    rows: [
      {
        account: "1000",
        average: "15000",
        monthly: null,
        count: 1,
        months: ["15000", "15000"],
      },
    ],
    unassigned: { average: "15000", count: 1 },
    expenses: [
      {
        journal: "purchase-1",
        line: 1,
        date: "2026-08-20",
        description: "Synthetic groceries",
        amount: "12000",
        expense_account: "5000",
        bucket: "",
        basis: "unassigned",
      },
    ],
    plan: {
      revision: "one",
      buckets: [
        { account: "1000", monthly: null, expense_accounts: [] as string[] },
      ],
      assignments: [],
    },
    accounts: [
      {
        code: "1000",
        name: "Emergency Savings",
        type: "ASSET",
        subtype: "BANK",
      },
      { code: "5000", name: "Groceries", type: "EXPENSE", subtype: "" },
    ],
  };
  await page.route("**/api/books/companies/example/budget", (r) =>
    r.fulfill({ json: budget }),
  );
  let saved: { plan: typeof budget.plan } | undefined;
  await page.route("**/api/books/companies/example/budget/save", (r) => {
    saved = r.request().postDataJSON();
    return r.fulfill({
      status: 409,
      json: { error: "Budget changed. Reload before saving." },
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "Edit budgets" }).click();
  expect(await noPageScroll(page)).toBe(true);
  await page
    .getByRole("textbox", { name: "Monthly budget for Checking" })
    .fill("0");
  await page
    .getByRole("button", { name: "Spending assignments", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Bucket for Groceries", exact: true })
    .click();
  await page
    .getByRole("option", { name: "Emergency Savings", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Review purchase assignments (1)" })
    .click();
  await page
    .getByRole("combobox", { name: /Bucket for Synthetic groceries/ })
    .click();
  await page
    .getByRole("option", { name: "Emergency Savings", exact: true })
    .click();
  expect(await noPageScroll(page)).toBe(true);
  await page.getByRole("button", { name: "Save budgets", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "Budget changed" }),
  ).toBeVisible();
  expect(saved?.plan.revision).toBe("one");
  expect(saved?.plan.buckets.find((b) => b.account === "1000")).toMatchObject({
    monthly: "0",
    expense_accounts: ["5000"],
  });
  expect(saved?.plan.assignments).toEqual([
    { journal: "purchase-1", line: 1, account: "1000" },
  ]);
  await expect(
    page.getByRole("textbox", { name: "Monthly budget for Checking" }),
  ).toHaveValue("0");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "Monthly budget for Checking" }),
  ).toHaveCount(0);
  await expect(page.getByTestId("cash-row-1000")).toContainText("Not set");
});

test("connected entity survives reload and falls back when access is removed", async ({
  page,
}) => {
  await connectedHousehold(page);
  const other = {
    key: "other",
    name: "Other Entity",
    currency: "USD",
    basis: "ACCRUAL",
  };
  const selected = {
    key: "example",
    name: "Example Household",
    currency: "USD",
    basis: "ACCRUAL",
  };
  let allowed = [other, selected];
  const requests: string[] = [];
  await page.route("**/api/books/companies", (r) => {
    if (allowed.length === 1) requests.length = 0;
    return r.fulfill({ json: allowed });
  });
  await page.route("**/api/books/companies/other/**", (r) =>
    r.request().url().includes("/budget")
      ? r.fulfill({ status: 403, json: { error: "Budgets unavailable" } })
      : r.fulfill({ json: { accounts: [] } }),
  );
  await page.goto("/");
  await page
    .getByRole("button", { name: "Select entity, current: Other Entity" })
    .click();
  await page.getByRole("menuitem", { name: "Example Household" }).click();
  await expect(page.getByTestId("cash-row-1000")).toContainText("Checking");
  await page.reload();
  await expect(
    page.getByRole("button", {
      name: "Select entity, current: Example Household",
    }),
  ).toBeVisible();
  await expect(page.getByTestId("cash-row-1000")).toContainText("Checking");
  allowed = [other];
  page.on("request", (r) => requests.push(r.url()));
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Select entity, current: Other Entity" }),
  ).toBeVisible();
  await expect(page.getByTestId("cash-row-1000")).toHaveCount(0);
  expect(requests.some((url) => url.includes("/companies/example/"))).toBe(
    false,
  );
});

test("entity switching still works when browser storage is blocked", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, "localStorage", {
      get() {
        throw new DOMException("Storage blocked", "SecurityError");
      },
    });
  });
  await page.goto("/");
  await page
    .getByRole("button", { name: "Select entity, current: Maple Household" })
    .click();
  await page.getByRole("menuitem", { name: "Example Studio" }).click();
  await expect(page.getByTestId("cash-row-1000")).toContainText(
    "Operating account",
  );
  await page.reload();
  await expect(
    page.getByRole("button", {
      name: "Select entity, current: Maple Household",
    }),
  ).toBeVisible();
});

test("budget edits include bank rows that load after editing begins", async ({
  page,
}) => {
  await connectedHousehold(page);
  await page.route("**/api/config", (r) =>
    r.fulfill({ json: { demo: false } }),
  );
  let release!: () => void;
  const ready = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/books/companies/example/reports/**", async (r) => {
    await ready;
    await r.fallback();
  });
  const budget = {
    can_edit: true,
    months: ["2026-07", "2026-08"],
    rows: [],
    unassigned: { average: "0", count: 0 },
    expenses: [],
    accounts: [
      { code: "1000", name: "Checking", type: "ASSET", subtype: "BANK" },
    ],
    plan: { revision: "one", buckets: [], assignments: [] },
  };
  await page.route("**/api/books/companies/example/budget", (r) =>
    r.fulfill({ json: budget }),
  );
  let saved:
    | { plan: { buckets: { account: string; monthly: string | null }[] } }
    | undefined;
  await page.route("**/api/books/companies/example/budget/save", (r) => {
    saved = r.request().postDataJSON();
    return r.fulfill({ json: budget });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "Edit budgets", exact: true }).click();
  await expect(page.getByRole("textbox")).toHaveCount(0);
  release();
  const input = page.getByRole("textbox", {
    name: "Monthly budget for Extra bank",
    exact: true,
  });
  await input.fill("123.45");
  await page.getByRole("button", { name: "Save budgets", exact: true }).click();
  await expect(input).toHaveCount(0);
  expect(saved?.plan.buckets.find((b) => b.account === "1099")?.monthly).toBe(
    "12345",
  );
});

test("a late budget read cannot replace a successful save", async ({
  page,
}) => {
  await connectedHousehold(page);
  await page.route("**/api/config", (r) =>
    r.fulfill({ json: { demo: false } }),
  );
  let releaseLedger!: () => void;
  const ledgerReady = new Promise<void>((resolve) => {
    releaseLedger = resolve;
  });
  await page.route("**/api/books/companies/example/reports/**", async (r) => {
    await ledgerReady;
    await r.fallback();
  });
  const budget = (monthly: string, revision: string) => ({
    can_edit: true,
    months: ["2026-07", "2026-08"],
    rows: [{ account: "1099", monthly, average: "10000", count: 1 }],
    unassigned: { average: "0", count: 0 },
    expenses: [],
    accounts: [
      { code: "1099", name: "Extra bank", type: "ASSET", subtype: "BANK" },
    ],
    plan: {
      revision,
      buckets: [{ account: "1099", monthly, expense_accounts: [] }],
      assignments: [],
    },
  });
  let releaseRead!: () => void, readStarted!: () => void;
  const readReady = new Promise<void>((resolve) => {
    releaseRead = resolve;
  });
  const reading = new Promise<void>((resolve) => {
    readStarted = resolve;
  });
  let reads = 0;
  await page.route("**/api/books/companies/example/budget", async (r) => {
    if (++reads > 1) {
      readStarted();
      await readReady;
    }
    await r.fulfill({ json: budget("20000", "one") });
  });
  await page.route("**/api/books/companies/example/budget/save", (r) =>
    r.fulfill({ json: budget("17550", "two") }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "Edit budgets", exact: true }).click();
  releaseLedger();
  await reading;
  await page
    .getByRole("textbox", {
      name: "Monthly budget for Extra bank",
      exact: true,
    })
    .fill("175.50");
  await page.getByRole("button", { name: "Save budgets", exact: true }).click();
  const row = page.getByTestId("cash-row-1099");
  await expect(row).toContainText("$175.50");
  const response = page.waitForResponse((r) => r.url().endsWith("/budget"));
  releaseRead();
  await (await response).finished();
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(row).toContainText("$175.50");
  await page.getByRole("button", { name: "Edit budgets", exact: true }).click();
  await expect(row.getByRole("textbox")).toHaveValue("175.50");
});
