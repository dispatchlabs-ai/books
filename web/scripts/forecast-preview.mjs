// Runs the connected web interface against a disposable Books home containing
// only the synthetic household. Use it to inspect forecast screens locally.
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { execFileSync, spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { createServer } from "node:net";
import { createBooksWebServer } from "../server.mjs";
import {
  householdAccounts,
  syntheticHousehold,
} from "./synthetic-household.mjs";

const dir = await mkdtemp(join(tmpdir(), "books-forecast-preview-"));
const env = {
  ...process.env,
  BOOKS_HOME: join(dir, "home"),
  BOOKS_CONFIG: join(dir, "home/books.toml"),
  BOOKS_ACTOR: "forecast-preview",
  CGO_ENABLED: "1",
  GOTOOLCHAIN: "auto",
};
let backend, web;
const stop = async () => {
  backend?.kill();
  if (web) await new Promise((r) => web.close(r));
  await rm(dir, { recursive: true, force: true });
  process.exit(0);
};
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
const bin = join(dir, "books");
execFileSync("go", ["build", "-o", bin, "./cmd/books"], {
  cwd: resolve(".."),
  env,
  stdio: "inherit",
});
const run = (...args) =>
  execFileSync(bin, ["--company", "maple", ...args], { env, stdio: "pipe" });
execFileSync(
  bin,
  [
    "init",
    "--company",
    "maple",
    "--name",
    "Maple Household",
    "--currency",
    "USD",
    "--start",
    "2026-01-01",
  ],
  { env, stdio: "pipe" },
);
for (const a of householdAccounts)
  run(
    "account",
    "add",
    a.kind === "card" ? "credit-card" : "bank",
    a.name,
    "--code",
    a.code,
  );
run(
  "receive",
  "8400.00",
  "Revenue",
  "Synthetic salary",
  "--to",
  "Income",
  "--date",
  "2026-09-01",
  "--key",
  "preview-salary",
);
run(
  "spend",
  "4850.00",
  "General Expense",
  "Synthetic mortgage",
  "--from",
  "Fixed Bills",
  "--date",
  "2026-09-01",
  "--key",
  "preview-mortgage",
);

const plans = syntheticHousehold();
const files = {};
for (const [scenario, plan] of Object.entries(plans)) {
  files[scenario] = join(dir, `${scenario}.json`);
  await writeFile(files[scenario], JSON.stringify(plan), { mode: 0o600 });
}
const probe = createServer();
await new Promise((r) => probe.listen(0, "127.0.0.1", r));
const port = probe.address().port;
await new Promise((r) => probe.close(r));
const token = randomBytes(32).toString("hex");
const serverConfig = join(dir, "server.json");
await writeFile(
  serverConfig,
  JSON.stringify({
    schema: "books.server/v2",
    listen: `127.0.0.1:${port}`,
    principals: [
      {
        id: "preview",
        token_sha256: createHash("sha256").update(token).digest("hex"),
        companies: { maple: ["read"] },
      },
    ],
  }),
  { mode: 0o600 },
);
backend = spawn(bin, ["serve", "--server-config", serverConfig], {
  env,
  stdio: "inherit",
});
for (let i = 0; i < 100; i++) {
  try {
    if (
      (
        await fetch(`http://127.0.0.1:${port}/v1/health`, {
          headers: { Authorization: `Bearer ${token}` },
        })
      ).ok
    )
      break;
  } catch {}
  await new Promise((r) => setTimeout(r, 50));
}
web = createBooksWebServer({
  upstream: `http://127.0.0.1:${port}`,
  token,
  forecastPlans: { maple: files },
});
const webPort = Number(process.env.BOOKS_WEB_PORT ?? 8790);
web.listen(webPort, "127.0.0.1", () =>
  console.log(`Synthetic forecast preview: http://127.0.0.1:${webPort}`),
);
