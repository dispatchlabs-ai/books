import { test, expect } from "@playwright/test";
import { createServer } from "node:https";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { createBooksWebServer } from "../server.mjs";
import { passwordRecord } from "../session-auth.mjs";

test.use({ ignoreHTTPSErrors: true });

test("ordinary browser forms preserve Origin through login and logout", async ({
  page,
}) => {
  const directory = mkdtempSync(join(tmpdir(), "books-login-browser-"));
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "rsa:2048",
      "-nodes",
      "-keyout",
      join(directory, "key.pem"),
      "-out",
      join(directory, "cert.pem"),
      "-days",
      "1",
      "-subj",
      "/CN=localhost",
    ],
    { stdio: "ignore" },
  );
  const proxyToken = "synthetic-proxy-token".repeat(3);
  const origins: string[] = [];
  let facade: ReturnType<typeof createBooksWebServer>;
  const edge = createServer(
    {
      key: readFileSync(join(directory, "key.pem")),
      cert: readFileSync(join(directory, "cert.pem")),
    },
    (req, res) => {
      if (req.method === "POST") origins.push(req.headers.origin!);
      req.headers["x-books-proxy-token"] = proxyToken;
      facade.emit("request", req, res);
    },
  );
  await new Promise<void>((resolve) => edge.listen(0, "127.0.0.1", resolve));
  const origin = `https://127.0.0.1:${(edge.address() as { port: number }).port}`;
  facade = createBooksWebServer({
    publicOrigin: origin,
    proxyToken,
    upstream: "https://api.example",
    token: "synthetic",
    sessionAuth: await passwordRecord("operator", "synthetic-password"),
    dist: process.cwd() + "/dist",
  });
  try {
    await page.goto(origin + "/login");
    await page.getByLabel("Username", { exact: true }).fill("operator");
    await page
      .getByLabel("Password", { exact: true })
      .fill("synthetic-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page).toHaveURL(origin + "/");
    expect(origins).toEqual([origin]);
    expect(
      await page.evaluate(async () => (await fetch("/api/config")).json()),
    ).toMatchObject({ session: true });
    // Use an ordinary form, not APIRequestContext (which fabricates no Origin).
    await page.setContent(
      '<form action="/logout" method="post"><button>Sign out</button></form>',
    );
    await page.getByRole("button", { name: "Sign out" }).click();
    await expect(page).toHaveURL(origin + "/login");
    await expect(
      page.getByRole("heading", { name: "Sign in to Books" }),
    ).toBeVisible();
    expect(origins).toEqual([origin, origin]);
  } finally {
    await page.close();
    await new Promise<void>((resolve) => edge.close(() => resolve()));
    rmSync(directory, { recursive: true, force: true });
  }
});
