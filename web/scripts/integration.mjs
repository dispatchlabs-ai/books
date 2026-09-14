import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { execFileSync, spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { createServer } from "node:net";
import assert from "node:assert/strict";
import { createBooksWebServer } from "../server.mjs";
const dir = await mkdtemp(join(tmpdir(), "books-web-test-"));
const env = {
  ...process.env,
  BOOKS_HOME: join(dir, "home"),
  BOOKS_CONFIG: join(dir, "home/books.toml"),
  BOOKS_ACTOR: "web-integration",
  CGO_ENABLED: "1",
  GOTOOLCHAIN: "auto",
};
let backend, web;
try {
  const bin = join(dir, "books");
  execFileSync("go", ["build", "-o", bin, "./cmd/books"], {
    cwd: resolve(".."),
    env,
    stdio: "pipe",
  });
  const run = (args) => execFileSync(bin, args, { env, stdio: "pipe" });
  run([
    "init",
    "--company",
    "web-example",
    "--name",
    "Example Studio",
    "--currency",
    "USD",
    "--start",
    "2026-01-01",
  ]);
  run([
    "--company",
    "web-example",
    "account",
    "add",
    "bank",
    "Checking",
    "--default-payment",
    "--default-deposit",
  ]);
  run([
    "--company",
    "web-example",
    "receive",
    "1000.00",
    "Revenue",
    "Synthetic receipt",
    "--to",
    "Checking",
    "--date",
    "2026-09-01",
    "--key",
    "web-receipt",
  ]);
  run([
    "--company",
    "web-example",
    "spend",
    "50.00",
    "General Expense",
    "Synthetic software",
    "--from",
    "Checking",
    "--date",
    "2026-09-02",
    "--key",
    "web-expense",
  ]);
  const probe = createServer();
  await new Promise((resolve) => probe.listen(0, "127.0.0.1", resolve));
  const port = probe.address().port;
  await new Promise((resolve) => probe.close(resolve));
  const token = randomBytes(32).toString("hex");
  const serverConfig = join(dir, "server.json");
  await writeFile(
    serverConfig,
    JSON.stringify({
      schema: "books.server/v2",
      listen: `127.0.0.1:${port}`,
      principals: [
        {
          id: "web-test",
          token_sha256: createHash("sha256").update(token).digest("hex"),
          companies: { "web-example": ["read"] },
        },
      ],
    }),
    { mode: 0o600 },
  );
  backend = spawn(bin, ["serve", "--server-config", serverConfig], {
    env,
    stdio: "ignore",
  });
  let ready = false;
  for (let i = 0; i < 100; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/v1/health`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      if (r.ok) {
        ready = true;
        break;
      }
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  assert.ok(ready, "isolated backend did not start");
  web = createBooksWebServer({ upstream: `http://127.0.0.1:${port}`, token });
  await new Promise((resolve) => web.listen(0, "127.0.0.1", resolve));
  const base = `http://127.0.0.1:${web.address().port}/api`;
  const get = async (path) => {
    const r = await fetch(base + path);
    assert.equal(r.status, 200);
    return r.json();
  };
  const companies = await get("/books/companies");
  assert.equal(companies[0].key, "web-example");
  const ledger = await get(
    "/books/companies/web-example/reports/general-ledger?from=2026-09-01&to=2026-09-13&include_zero=true",
  );
  const bank = ledger.accounts.find((a) => a.account.subtype === "BANK");
  assert.equal(bank.closing_balance.consolidated_cents, "95000");
  assert.equal(bank.lines.length, 2);
  const pl = await get(
    "/books/companies/web-example/reports/profit-loss?from=2026-09-01&to=2026-09-13",
  );
  assert.equal(pl.net_income.consolidated_cents, "95000");
  assert.equal(
    (
      await fetch(
        base +
          "/books/companies/foreign/reports/general-ledger?from=2026-09-01&to=2026-09-13",
      )
    ).status,
    404,
  );
  run(["--company", "web-example", "doctor"]);
  run(["--company", "web-example", "audit", "verify"]);
  console.log(
    "Real Books API → web facade integration passed with isolated synthetic data.",
  );
} finally {
  if (web) await new Promise((resolve) => web.close(resolve));
  if (backend && backend.exitCode === null) {
    backend.kill("SIGTERM");
    await new Promise((resolve) => backend.once("exit", resolve));
  }
  await rm(dir, { recursive: true, force: true });
}
