import { test, expect } from "@playwright/test";
test("overview, account search, scoped conversation, entity reset and planning", async ({
  page,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Your money, in view" }),
  ).toBeVisible();
  await expect(page.getByText("Demo · Synthetic data")).toBeVisible();
  // Demo mode has no cash plan, so there is no Outlook destination.
  await expect(
    page.getByRole("button", { name: "Outlook", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: /Everyday checking Cash account/ })
    .click();
  await expect(
    page.getByRole("heading", { name: "Everyday checking" }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Search transactions" })
    .fill("coffee");
  await expect(page.getByText("Coffee House", { exact: true })).toBeVisible();
  await expect(page.getByText("Paycheck", { exact: true })).not.toBeVisible();
  await page.getByRole("button", { name: "Ask about this account" }).click();
  await page
    .getByRole("textbox", { name: "Ask Books a question" })
    .fill("How much cash do we have?");
  await page.getByRole("button", { name: "Send question" }).click();
  await expect(page.getByText(/This demo has \$8,420.00/)).toBeVisible();
  await page
    .getByRole("button", { name: "Select entity" })
    .filter({ visible: true })
    .click();
  await page.getByRole("menuitem", { name: "Example Studio" }).click();
  await expect(
    page.getByText("Business overview", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/This demo has/)).not.toBeVisible();
  await page
    .getByRole("button", { name: "Select entity" })
    .filter({ visible: true })
    .click();
  await page.getByRole("menuitem", { name: "Maple Household" }).click();
  await page.getByRole("button", { name: /Planning the home repair/ }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("radio", { name: /This week/ }).check();
  await page.getByRole("button", { name: "Save demo plan" }).click();
  await expect(
    page.getByText("Repair plan saved", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByText("Repair plan saved", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: /Repair plan saved/ }).click();
  await expect(page.getByRole("radio", { name: /This week/ })).toBeChecked();
  await page.getByRole("button", { name: "Save demo plan" }).click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
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

test("connected history and conversations keep account scopes separate", async ({
  page,
}) => {
  const company = {
    key: "example",
    name: "Example entity",
    currency: "USD",
    basis: "accrual",
  };
  const sent: { account: string; messages: { content: string }[] }[] = [];
  const reportURLs: string[] = [];
  await page.route("**/api/config", (route) =>
    route.fulfill({ json: { demo: false, agent: true } }),
  );
  await page.route("**/api/books/companies", (route) =>
    route.fulfill({ json: [company] }),
  );
  await page.route("**/api/books/companies/example/reports/**", (route) => {
    reportURLs.push(route.request().url());
    return route.fulfill({
      json: route.request().url().includes("general-ledger")
        ? {
            accounts: ["Checking", "Savings"].map((name, i) => ({
              account: {
                id: name,
                code: String(1000 + i),
                name,
                subtype: "BANK",
              },
              opening_balance: { consolidated_cents: "100" },
              closing_balance: { consolidated_cents: "100" },
              lines: [],
            })),
          }
        : {
            total_revenue: { consolidated_cents: "0" },
            total_expenses: { consolidated_cents: "0" },
            net_income: { consolidated_cents: "0" },
          },
    });
  });
  await page.route("**/api/ask", (route) => {
    sent.push(route.request().postDataJSON());
    return route.fulfill({
      json: { text: "Answer for " + sent.at(-1)!.account },
    });
  });
  await page.goto("/");
  await page.getByText(/^Period:/).click();
  await page.getByLabel("Period start").fill("2024-06-01");
  await page.getByLabel("Period end").fill("2024-06-30");
  await page.getByRole("button", { name: "Apply period" }).click();
  await expect
    .poll(() =>
      reportURLs.some((url) => url.includes("from=2024-06-01&to=2024-06-30")),
    )
    .toBe(true);
  await page.getByRole("button", { name: /Checking Cash account/ }).click();
  await page.getByRole("button", { name: "Ask about this account" }).click();
  await page
    .getByRole("textbox", { name: "Ask Books a question" })
    .fill("Question for checking");
  await page.getByRole("button", { name: "Send question" }).click();
  await expect(
    page.getByText("Answer for 1000", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Overview", exact: true })
    .filter({ visible: true })
    .click();
  await page.getByRole("button", { name: /Savings Cash account/ }).click();
  await page.getByRole("button", { name: "Ask about this account" }).click();
  await expect(
    page.getByText("Answer for 1000", { exact: true }),
  ).not.toBeVisible();
  await page
    .getByRole("textbox", { name: "Ask Books a question" })
    .fill("Question for savings");
  await page.getByRole("button", { name: "Send question" }).click();
  await expect(
    page.getByText("Answer for 1001", { exact: true }),
  ).toBeVisible();
  expect(sent[1].messages).toEqual([
    { role: "user", content: "Question for savings" },
  ]);
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
  failFunded = false,
) {
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
  await page.route("**/api/books/companies/example/cash-forecast?**", (r) => {
    const scenario = new URL(r.request().url()).searchParams.get("scenario") as
      "baseline" | "funded";
    if (scenario === "funded" && failures > 0) {
      failures--;
      return r.fulfill({
        status: 404,
        json: { error: "Scenario unavailable" },
      });
    }
    return r.fulfill({ json: householdForecast[scenario] });
  });
}
const noPageScroll = (page: import("@playwright/test").Page) =>
  page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);

test("point-forward outlook answers coverage, reserves and account gaps", async ({
  page,
}, testInfo) => {
  await connectedHousehold(page);
  await page.goto("/");
  const summary = page
    .getByText("Looking ahead", { exact: true })
    .locator("xpath=ancestor::*[@data-slot='card'][1]");
  await expect(
    summary.getByText(
      "Planned spending is more than expected income in November.",
    ),
  ).toBeVisible();
  await expect(summary.getByText("+$275.00")).toBeVisible();
  await expect(
    summary.getByText(/Baseline: Kids Activities runs short from Nov 20/),
  ).toBeVisible();
  await summary.getByRole("button", { name: "Open outlook" }).click();

  await expect(
    page.getByRole("heading", { name: "Will income cover spending?" }),
  ).toBeVisible();
  await expect(
    page.getByText("Forecast · Estimates from a saved cash plan"),
  ).toBeVisible();
  await expect(page.getByText(/Period:/)).toHaveCount(0);
  // Oct: 5,000 − 3,000 − 1,200 = +800. Nov: 5,000 − 3,000 − 1,800 − 450 = −250.
  // Card payments and the partial Sep 30 paycheck are excluded from the rate.
  const months = page.getByRole("list", { name: "Full months" });
  await expect(
    months.getByRole("button", {
      name: /^October: income \$5,000\.00, spending \$4,200\.00, left over \$800\.00/,
    }),
  ).toBeVisible();
  await expect(
    months.getByRole("button", {
      name: /^November: income \$5,000\.00, spending \$5,250\.00, short \$250\.00/,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", {
      name: "Planned spending is more than expected income in November.",
    }),
  ).toBeVisible();
  await expect(page.getByText("+$275.00", { exact: true })).toBeVisible();
  await expect(
    page.getByText("Sep 30 · Partial month, not included in the average"),
  ).toBeVisible();
  await months.getByRole("button", { name: /^November:/ }).click();
  await expect(months.getByText("Kids activities")).toBeVisible();
  await expect(
    page.getByText(
      "This scenario doesn’t move money into or out of reserve accounts.",
    ),
  ).toBeVisible();
  await expect(
    page.getByText(/Baseline includes no proposed transfers/),
  ).toBeVisible();

  await page.getByRole("radio", { name: "Funded" }).click();
  await expect(page.getByText("Example — proposed funding")).toBeVisible();
  await expect(
    page.getByText(/draw on existing balances: −\$175\.00 a month on average/),
  ).toBeVisible();
  await expect(
    page.getByText("They don’t show whether full reserve targets are met.", {
      exact: false,
    }),
  ).toBeVisible();
  const gap = page.getByRole("button", {
    name: "View daily balances for Kids Activities",
  });
  await expect(
    page.getByText(/Below its \$0\.00 floor from Nov 20/),
  ).toBeVisible();
  await gap.click();
  await expect(
    page.getByRole("heading", { name: "Daily balances by account" }),
  ).toBeFocused();
  await expect(
    page.getByRole("combobox", { name: "Bank account" }),
  ).toContainText("Kids Activities");
  await expect(
    page.getByRole("radio", { name: /Below floor 11/ }),
  ).toBeChecked();
  const day = page.getByRole("button", { name: /Fri, Nov 20/ });
  await expect(day).toHaveAttribute("aria-expanded", "true");
  await page.getByText("Evidence", { exact: true }).first().click();
  await expect(
    page.getByText("Evidence: Synthetic evidence for Team registration"),
  ).toBeVisible();
  expect(await noPageScroll(page)).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("outlook-gap.png"),
    fullPage: true,
  });

  await page.getByRole("combobox", { name: "Bank account" }).click();
  await page.getByRole("option", { name: /Home Reserve/ }).click();
  await expect(
    page.getByRole("radio", { name: /With activity 2/ }),
  ).toBeChecked();
  await expect(
    page.getByRole("radio", { name: /Below floor 0/ }),
  ).toBeDisabled();

  // Scenario and account survive leaving and returning.
  await page
    .getByRole("button", { name: "Overview", exact: true })
    .filter({ visible: true })
    .click();
  await page
    .getByRole("button", { name: "Outlook", exact: true })
    .filter({ visible: true })
    .click();
  await expect(page.getByRole("radio", { name: "Funded" })).toBeChecked();
  await expect(
    page.getByRole("combobox", { name: "Bank account" }),
  ).toContainText("Home Reserve");
  expect(await noPageScroll(page)).toBe(true);
});

test("outlook recovers from a failed scenario and links from account detail", async ({
  page,
}) => {
  await connectedHousehold(page, true);
  await page.goto("/");
  await page
    .getByRole("button", { name: "Outlook", exact: true })
    .filter({ visible: true })
    .click();
  await page.getByRole("radio", { name: "Funded" }).click();
  await expect(
    page.getByRole("heading", { name: "Funded couldn’t load" }),
  ).toBeVisible();
  await expect(page.getByText("Scenario unavailable")).toBeVisible();
  await page.getByRole("button", { name: "Back to Baseline" }).click();
  await expect(page.getByText("Example — baseline")).toBeVisible();
  await page.getByRole("radio", { name: "Funded" }).click();
  await expect(
    page.getByRole("heading", { name: "Funded couldn’t load" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByText("Example — proposed funding")).toBeVisible();

  await page
    .getByRole("button", { name: "Overview", exact: true })
    .filter({ visible: true })
    .click();
  await page
    .getByRole("button", { name: /Kids Activities Cash account/ })
    .click();
  await page.getByRole("tab", { name: "Outlook" }).click();
  await page
    .getByRole("button", { name: "View daily balances in Outlook" })
    .click();
  await expect(
    page.getByRole("heading", { name: "Daily balances by account" }),
  ).toBeFocused();
  await expect(
    page.getByRole("combobox", { name: "Bank account" }),
  ).toContainText("Kids Activities");
  expect(await noPageScroll(page)).toBe(true);
});
