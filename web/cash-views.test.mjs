import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, stat, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createBooksWebServer } from "./server.mjs";

async function fixture(run) {
  const dir = await mkdtemp(join(tmpdir(), "books-views-"));
  const file = join(dir, "views.json");
  const calls = [];
  const serve = async (fn, extra = {}) => {
    const server = createBooksWebServer({
      upstream: "https://books.example",
      token: "synthetic-token",
      cashViewsFile: file,
      fetcher: async (url, init) => {
        calls.push({ url, init });
        return Response.json({
          schema: "books.api/v1",
          ok: true,
          data: [{ key: "a" }, { key: "b" }],
        });
      },
      ...extra,
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const root = `http://127.0.0.1:${server.address().port}`;
    const read = async (company = "a") =>
      (await fetch(`${root}/api/books/companies/${company}/cash-view`)).json();
    const save = (company, data, headers = {}) =>
      fetch(`${root}/api/books/companies/${company}/cash-view`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...headers },
        body: JSON.stringify(data),
      });
    try {
      await fn({ root, read, save });
    } finally {
      await new Promise((resolve) => server.close(resolve));
    }
  };
  try {
    await run({ serve, file, calls });
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

test("cash views persist per entity across server restarts without ledger writes", () =>
  fixture(async ({ serve, file, calls }) => {
    await serve(async ({ read, save, root }) => {
      assert.equal(
        (await (await fetch(root + "/api/config")).json()).cashViews,
        true,
      );
      const [a, b] = await Promise.all([read("a"), read("b")]);
      const updates = await Promise.all([
        save("a", { ...a, order: ["1030", "1000"], hidden: ["1000"] }),
        save("b", {
          ...b,
          order: ["BANK/01", "CASH USD", "ÉPARGNE"],
          hidden: ["CASH USD"],
        }),
      ]);
      assert.deepEqual(
        updates.map((r) => r.status),
        [200, 200],
      );
      assert.equal(
        (await save("a", { ...a, order: [], hidden: [] })).status,
        409,
      );
    });
    await serve(async ({ read }) => {
      assert.deepEqual((await read("a")).order, ["1030", "1000"]);
      assert.deepEqual((await read("a")).hidden, ["1000"]);
      assert.deepEqual((await read("b")).order, [
        "BANK/01",
        "CASH USD",
        "ÉPARGNE",
      ]);
    });
    assert.equal((await stat(file)).mode & 0o777, 0o600);
    assert.ok(
      calls.every(
        (c) => c.url === "https://books.example/v1/companies" && !c.init.method,
      ),
    );
    assert.ok(
      calls.every(
        (c) => c.init.headers.Authorization === "Bearer synthetic-token",
      ),
    );
  }));

test("cash views enforce entity access, origin, method and bounded valid payloads", () =>
  fixture(async ({ serve, file }) => {
    await serve(async ({ read, save, root }) => {
      const view = await read();
      assert.equal((await save("foreign", view)).status, 403);
      assert.equal(
        (await save("a", view, { Origin: "https://evil.example" })).status,
        403,
      );
      assert.equal(
        (await save("a", view, { "Content-Type": "text/plain" })).status,
        415,
      );
      assert.equal(
        (
          await fetch(root + "/api/books/companies/a/cash-view", {
            method: "DELETE",
          })
        ).status,
        405,
      );
      for (const order of [["x", "x"], [""], [4], Array(10001).fill("x"), null])
        assert.equal((await save("a", { ...view, order })).status, 400);
      assert.equal(
        (await save("a", { ...view, hidden: ["x".repeat(256001)] })).status,
        400,
      );
      assert.deepEqual(await read(), view);
    });
    await assert.rejects(readFile(file), { code: "ENOENT" });
  }));

test("cash view store corruption and upstream authorization failure never overwrite preferences", () =>
  fixture(async ({ serve, file }) => {
    await writeFile(file, "corrupt");
    await serve(async ({ root, save }) => {
      assert.equal(
        (await fetch(root + "/api/books/companies/a/cash-view")).status,
        502,
      );
      assert.equal(
        (await save("a", { revision: "", order: [], hidden: [] })).status,
        502,
      );
    });
    await serve(
      async ({ save }) => {
        assert.equal(
          (await save("a", { revision: "", order: [], hidden: [] })).status,
          502,
        );
      },
      {
        fetcher: async () =>
          Response.json({ error: "offline" }, { status: 503 }),
      },
    );
    assert.equal(await readFile(file, "utf8"), "corrupt");
  }));
