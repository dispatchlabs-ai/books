import { test, expect } from "@playwright/test";
test("overview, account search, scoped conversation, entity reset and planning", async ({
  page,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Your money, in view" }),
  ).toBeVisible();
  await expect(page.getByText("Demo · Synthetic data")).toBeVisible();
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

test("daily per-bank forecast, movement evidence and scenario recovery", async ({
  page,
}, testInfo) => {
  const company = {
    key: "example",
    name: "Example Household",
    entity: "EXAMPLE",
    book: "EXAMPLE",
    currency: "USD",
    basis: "ACCRUAL",
  };
  await page.route("**/api/config", (r) =>
    r.fulfill({ json: { demo: false, agent: false } }),
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
                  id: "bank",
                  code: "1000",
                  name: "Checking",
                  subtype: "BANK",
                },
                opening_balance: { consolidated_cents: "100000" },
                closing_balance: { consolidated_cents: "100000" },
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
  const event = {
    id: "rent",
    date: "2026-09-02",
    name: "Home rent",
    account: "1000",
    amount: "90000",
    status: "confirmed",
    evidence: "Synthetic lease",
    category: "Housing",
  };
  const forecast = {
    digest: "synthetic-digest",
    scenarios: ["baseline", "funded"],
    plan: {
      name: "Synthetic",
      as_of: "2026-09-01",
      through: "2026-09-03",
      currency: "USD",
      accounts: [
        {
          code: "1000",
          name: "Checking",
          kind: "bank",
          reserved: false,
          opening: "100000",
          floor: "20000",
          evidence: "Synthetic bank snapshot",
        },
        {
          code: "1001",
          name: "Savings",
          kind: "bank",
          reserved: true,
          opening: "50000",
          floor: "0",
          evidence: "Synthetic snapshot",
        },
      ],
      assumptions: ["Synthetic daily example"],
    },
    days: [1, 2, 3].flatMap((n) => [
      {
        date: `2026-09-0${n}`,
        account: "1000",
        opening: n === 3 ? "10000" : "100000",
        inflow: "0",
        outflow: n === 2 ? "90000" : "0",
        closing: n === 1 ? "100000" : "10000",
        floor: "20000",
        shortfall: n === 1 ? "0" : "10000",
        movements:
          n === 2 ? [{ event, delta: "-90000", balance: "10000" }] : [],
      },
      {
        date: `2026-09-0${n}`,
        account: "1001",
        opening: "50000",
        inflow: "0",
        outflow: "0",
        closing: "50000",
        floor: "0",
        shortfall: "0",
        movements: [],
      },
    ]),
    lows: [
      {
        account: "1000",
        date: "2026-09-02",
        balance: "10000",
        shortfall: "10000",
      },
      { account: "1001", date: "2026-09-01", balance: "50000", shortfall: "0" },
    ],
    warnings: [],
    variances: [],
  };
  await page.route("**/api/books/companies/example/cash-forecast?**", (r) =>
    r.request().url().includes("scenario=funded")
      ? r.fulfill({ status: 404, json: { error: "Scenario unavailable" } })
      : r.fulfill({ json: forecast }),
  );
  await page.goto("/");
  const view = page.getByRole("region", { name: "Daily cash projection" });
  await expect(view).toBeVisible();
  await view.getByRole("button", { name: "2026-09-02", exact: true }).click();
  await view.getByText("Home rent ·").click();
  await expect(view.getByText("Evidence: Synthetic lease")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("daily-cash.png"),
    fullPage: true,
  });
  await page.getByLabel("Scenario", { exact: true }).selectOption("funded");
  await expect(page.getByText("Scenario unavailable")).toBeVisible();
  await page.getByLabel("Scenario", { exact: true }).selectOption("baseline");
  await expect(view).toBeVisible();
  await page.getByLabel("Bank account").selectOption("1001");
  await expect(
    view.getByText("No movements. Balance carries forward."),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
