import { request as httpRequest } from "node:http";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createBooksWebServer } from "./server.mjs";
import { passwordRecord, createSessionAuth } from "./session-auth.mjs";
test("session boundary, feedback persistence, rejection paths and logout", async () => {
  const dir = await mkdtemp(join(tmpdir(), "books-login-"));
  const feedback = Object.fromEntries(
    ["sources", "snapshot", "answers"].map((k) => [k, join(dir, k + ".json")]),
  );
  await writeFile(
    feedback.sources,
    JSON.stringify({ records: [{ source: { source_uid: "pending" } }] }),
  );
  await writeFile(
    feedback.snapshot,
    JSON.stringify({
      transactions: [{ source_uid: "pending" }, { source_uid: "resolved" }],
    }),
  );
  await writeFile(
    feedback.answers,
    JSON.stringify({ resolved: "Retain note" }),
  );
  const sessionAuth = await passwordRecord("operator", "synthetic");
  const server = createBooksWebServer({
    publicOrigin: "https://web.example",
    proxyToken: "x".repeat(32),
    upstream: "https://api.example",
    token: "synthetic",
    sessionAuth,
    feedback,
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const headers = {
    Host: "web.example",
    "X-Books-Proxy-Token": "x".repeat(32),
  };
  const call = (path, options = {}) =>
    new Promise((resolve, reject) => {
      const req = httpRequest(
        `http://127.0.0.1:${server.address().port}${path}`,
        {
          method: options.method || "GET",
          headers: { ...headers, ...options.headers },
        },
        (res) => {
          const chunks = [];
          res.on("data", (c) => chunks.push(c));
          res.on("end", () =>
            resolve(
              new Response(Buffer.concat(chunks), {
                status: res.statusCode,
                headers: Object.fromEntries(
                  Object.entries(res.headers).map(([k, v]) => [
                    k,
                    Array.isArray(v) ? v.join(", ") : v,
                  ]),
                ),
              }),
            ),
          );
        },
      );
      req.on("error", reject);
      req.end(options.body?.toString());
    });
  const login = (password) =>
    call("/login", {
      method: "POST",
      headers: {
        Origin: "https://web.example",
        "Content-Type": "application/x-www-form-urlencoded",
      },
      body: new URLSearchParams({ username: "operator", password }),
    });
  try {
    assert.equal((await call("/")).status, 303);
    const page = await call("/login");
    assert.match(page.headers.get("content-type"), /text\/html/);
    assert.match(await page.text(), /Sign in to Books/);
    for (const path of [
      "/api/config",
      "/api/books/companies/example/cash-view",
      "/api/books/companies",
      "/feedback/api/items",
    ])
      assert.equal((await call(path)).status, 401);
    assert.equal(
      (await call("/feedback/api/answers", { method: "POST", body: "{}" }))
        .status,
      401,
    );
    assert.equal((await login("wrong")).status, 401);
    const signed = await login("synthetic");
    assert.equal(signed.status, 303);
    const cookie = signed.headers.get("set-cookie");
    assert.match(cookie, /Secure; HttpOnly; SameSite=Strict/);
    headers.Cookie = cookie.split(";")[0];
    assert.equal((await call("/api/config")).status, 200);
    assert.equal((await call("/api/ask", { method: "POST" })).status, 403);
    assert.equal(
      (
        await call("/logout", {
          method: "POST",
          headers: { Origin: "https://evil.example" },
        })
      ).status,
      403,
    );
    const initial = await call("/feedback/api/items");
    const answerRevision = initial.headers.get("etag");
    const data = await initial.json();
    assert.equal(data.answers.resolved, "Retain note");
    const answers = { resolved: "Retain note", pending: "Synthetic purpose" };
    const save = (body) =>
      call("/feedback/api/answers", {
        method: "POST",
        headers: { Origin: "https://web.example", "If-Match": answerRevision },
        body,
      });
    assert.equal((await save(JSON.stringify(answers))).status, 200);
    assert.equal((await save('{"unknown":"no"}')).status, 400);
    assert.equal((await save("[]")).status, 400);
    assert.equal(
      (await save(JSON.stringify({ pending: "stale" }))).status,
      409,
    );
    assert.deepEqual(
      JSON.parse(await readFile(feedback.answers, "utf8")),
      answers,
    );
    assert.equal(
      (
        await call("/logout", {
          method: "POST",
          headers: { Origin: "https://web.example" },
        })
      ).status,
      303,
    );
    assert.equal((await call("/api/config")).status, 401);
    for (let i = 0; i < 4; i++) await login("wrong");
    assert.equal((await login("synthetic")).status, 429);
  } finally {
    await new Promise((r) => server.close(r));
    await rm(dir, { recursive: true });
  }
});
test("session expires after twelve hours", async () => {
  let time = 1000000;
  const auth = createSessionAuth(
    await passwordRecord("operator", "synthetic"),
    { now: () => time },
  );
  let cookie;
  const res = {
    setHeader(k, v) {
      if (k === "Set-Cookie") cookie = v;
    },
    writeHead() {},
    end() {},
  };
  const request = {
    method: "POST",
    headers: {
      origin: "https://web.example",
      "content-type": "application/x-www-form-urlencoded",
    },
    async *[Symbol.asyncIterator]() {
      yield "username=operator&password=synthetic";
    },
  };
  await auth(
    request,
    res,
    new URL("https://web.example/login"),
    "https://web.example",
  );
  const get = { method: "GET", headers: { cookie: cookie.split(";")[0] } };
  assert.equal(
    await auth(
      get,
      res,
      new URL("https://web.example/api/config"),
      "https://web.example",
    ),
    false,
  );
  time += 43200001;
  assert.equal(
    await auth(
      get,
      res,
      new URL("https://web.example/api/config"),
      "https://web.example",
    ),
    true,
  );
});
