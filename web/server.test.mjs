import { request as httpRequest } from "node:http";
import { test } from "node:test";
import assert from "node:assert/strict";
import { createBooksWebServer } from "./server.mjs";
async function fixture(config, run) {
  const server = createBooksWebServer(config);
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    await run(`http://127.0.0.1:${server.address().port}`);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}
test("demo config does not fabricate connected data", async () =>
  fixture({}, async (url) => {
    assert.deepEqual(await (await fetch(url + "/api/config")).json(), {
      demo: true,
      agent: false,
      forecasts: {},
    });
    assert.equal((await fetch(url + "/api/books/companies")).status, 503);
  }));
test("read-only allowlist, host, origin and server-side credentials", async () => {
  const calls = [];
  await fixture(
    {
      upstream: "https://books.example",
      token: "private-test-token",
      fetcher: async (url, init) => {
        calls.push({ url, init });
        return Response.json({ schema: "books.api/v1", ok: true, data: [] });
      },
    },
    async (url) => {
      const response = await fetch(url + "/api/books/companies");
      assert.deepEqual(await response.json(), []);
      assert.equal(
        calls[0].init.headers.Authorization,
        "Bearer private-test-token",
      );
      assert.equal(calls[0].init.redirect, "error");
      assert.equal(
        (
          await fetch(
            url +
              "/api/books/companies/example/reports/profit-loss?from=2026-09-01&to=2026-09-13",
          )
        ).status,
        200,
      );
      for (const path of [
        "/api/books/companies/example/transactions/spend",
        "/api/books/companies/example/operations/journal_post",
        "/api/books/databases/example",
      ])
        assert.equal((await fetch(url + path, { method: "POST" })).status, 404);
      assert.equal(
        await new Promise((resolve) => {
          const req = httpRequest(
            url + "/api/config",
            { headers: { Host: "attacker.example" } },
            (res) => {
              res.resume();
              resolve(res.statusCode);
            },
          );
          req.end();
        }),
        403,
      );
      assert.equal(
        (
          await fetch(url + "/api/config", {
            headers: { Origin: "https://attacker.example" },
          })
        ).status,
        403,
      );
      assert.equal(calls.length, 2);
    },
  );
});
test("AI rejects foreign entities and invalid input before sending data", async () => {
  let agentCalls = 0;
  await fixture(
    {
      upstream: "https://books.example",
      token: "secret",
      agentURL: "https://agent.example/ask",
      fetcher: async (url) => {
        if (url.includes("agent.example")) {
          agentCalls++;
          return Response.json({ text: "answer" });
        }
        return Response.json({
          schema: "books.api/v1",
          ok: true,
          data: [{ key: "example" }],
        });
      },
    },
    async (url) => {
      const send = (body) =>
        fetch(url + "/api/ask", { method: "POST", body: JSON.stringify(body) });
      assert.equal(
        (
          await send({
            company: "foreign",
            messages: [{ role: "user", content: "Hello" }],
          })
        ).status,
        403,
      );
      assert.equal(
        (
          await send({
            company: "example",
            messages: [{ role: "system", content: "Hello" }],
          })
        ).status,
        400,
      );
      assert.deepEqual(
        await (
          await send({
            company: "example",
            messages: [{ role: "user", content: "Hello" }],
          })
        ).json(),
        { text: "answer" },
      );
      assert.equal(agentCalls, 1);
    },
  );
});
test("bad upstream envelopes and redirects never become successful data", async () =>
  fixture(
    {
      upstream: "https://books.example",
      token: "secret",
      fetcher: async () => Response.json({ token: "must-not-leak" }),
    },
    async (url) => {
      const response = await fetch(url + "/api/books/companies");
      assert.equal(response.status, 502);
      assert.equal((await response.text()).includes("must-not-leak"), false);
    },
  ));
test("configuration fails closed instead of downgrading to demo", () => {
  assert.throws(() =>
    createBooksWebServer({ upstream: "https://books.example" }),
  );
  assert.throws(() =>
    createBooksWebServer({
      upstream: "http://remote.example",
      token: "secret",
    }),
  );
});

test("hosted mode requires authenticated proxy and exact HTTPS origin", async () => {
  const config = {
    publicOrigin: "https://books.example",
    proxyToken: "p".repeat(48),
    upstream: "https://api.example",
    token: "read-token",
  };
  await fixture(config, async (url) => {
    const send = (headers) =>
      new Promise((resolve) => {
        const req = httpRequest(url + "/api/config", { headers }, (res) => {
          let body = "";
          res.on("data", (c) => (body += c));
          res.on("end", () =>
            resolve({ status: res.statusCode, body: JSON.parse(body) }),
          );
        });
        req.end();
      });
    assert.equal((await send({ Host: "books.example" })).status, 403);
    assert.equal(
      (await send({ Host: "books.example", "X-Books-Proxy-Token": "wrong" }))
        .status,
      403,
    );
    assert.equal(
      (
        await send({
          Host: "attacker.example",
          "X-Books-Proxy-Token": config.proxyToken,
        })
      ).status,
      403,
    );
    assert.equal(
      (
        await send({
          Host: "books.example",
          "X-Books-Proxy-Token": config.proxyToken,
          Origin: "https://attacker.example",
        })
      ).status,
      403,
    );
    assert.deepEqual(
      await send({
        Host: "books.example",
        "X-Books-Proxy-Token": config.proxyToken,
        Origin: config.publicOrigin,
      }),
      { status: 200, body: { demo: false, agent: false, forecasts: {} } },
    );
  });
  assert.throws(() =>
    createBooksWebServer({ publicOrigin: config.publicOrigin }),
  );
  assert.throws(() =>
    createBooksWebServer({ ...config, upstream: undefined, token: undefined }),
  );
});

test("forecast plan paths are operator-owned and backend enforces company access", async () => {
  const { mkdtemp, writeFile, rm } = await import("node:fs/promises");
  const { tmpdir } = await import("node:os");
  const directory = await mkdtemp(tmpdir() + "/books-web-forecast-");
  try {
    const file = directory + "/plan.json";
    await writeFile(file, JSON.stringify({ version: "books.cash-plan/v1" }));
    let called = 0;
    await fixture(
      {
        upstream: "https://books.example",
        token: "test-token",
        forecastPlans: { example: { baseline: file } },
        fetcher: async (url, init) => {
          called++;
          assert.equal(
            url,
            "https://books.example/v1/companies/example/operations/cash_forecast",
          );
          assert.equal(init.method, "POST");
          assert.equal(JSON.parse(init.body).version, "books.cash-plan/v1");
          return Response.json({
            schema: "books.api/v1",
            ok: true,
            data: { digest: "synthetic" },
          });
        },
      },
      async (url) => {
        const config = await (await fetch(url + "/api/config")).json();
        assert.deepEqual(config.forecasts, { example: ["baseline"] });
        assert.doesNotMatch(JSON.stringify(config), /plan\.json|books-web-forecast/);
        const response = await fetch(
          url + "/api/books/companies/example/cash-forecast",
        );
        assert.equal(response.status, 200);
        assert.deepEqual((await response.json()).scenarios, ["baseline"]);
        assert.equal(
          (
            await fetch(
              url +
                "/api/books/companies/example/cash-forecast?scenario=../../private",
            )
          ).status,
          404,
        );
        assert.equal(
          (await fetch(url + "/api/books/companies/foreign/cash-forecast"))
            .status,
          404,
        );
        assert.equal(called, 1);
      },
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
