import { createHash, randomBytes } from "node:crypto";
import { readFile, writeFile, rename, mkdir, unlink } from "node:fs/promises";
import { dirname } from "node:path";
import { readBody } from "./session-auth.mjs";

const empty = () => ({ order: [], hidden: [] });
const revision = (view) =>
  createHash("sha256").update(JSON.stringify(view)).digest("hex");
function validList(value) {
  return (
    Array.isArray(value) &&
    value.length <= 10000 &&
    value.every(
      (v) => typeof v === "string" && v.length > 0 && v.length <= 128,
    ) &&
    new Set(value).size === value.length
  );
}
function validView(value) {
  return value && validList(value.order) && validList(value.hidden);
}

// One web process owns this display-only store. Revisions are per entity;
// serializing read/modify/rename also preserves updates to other entities.
export function createCashViews({ file, authorize }) {
  let queue = Promise.resolve();
  const load = async () => {
    let raw;
    try {
      raw = await readFile(file, "utf8");
    } catch (e) {
      if (e.code === "ENOENT") return { version: 1, views: {} };
      throw e;
    }
    const store = JSON.parse(raw);
    if (
      store.version !== 1 ||
      !store.views ||
      Array.isArray(store.views) ||
      typeof store.views !== "object" ||
      !Object.values(store.views).every(validView)
    )
      throw new Error("Invalid account view store.");
    return store;
  };
  const get = (store, company) =>
    Object.hasOwn(store.views, company) ? store.views[company] : empty();
  return async (req, res, url) => {
    const match = url.pathname.match(
      /^\/api\/books\/companies\/([A-Za-z0-9_-]+)\/cash-view$/,
    );
    if (!match) return false;
    const reply = (status, body) => {
      res.writeHead(status, {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      });
      res.end(JSON.stringify(body));
    };
    const company = match[1];
    if (!(await authorize(company))) {
      reply(403, { error: "Entity is not accessible." });
      return true;
    }
    if (req.method === "GET") {
      const view = get(await load(), company);
      reply(200, { ...view, revision: revision(view) });
      return true;
    }
    if (req.method !== "POST") {
      reply(405, { error: "Method not allowed." });
      return true;
    }
    if (!req.headers["content-type"]?.startsWith("application/json")) {
      reply(415, { error: "JSON required." });
      return true;
    }
    let input;
    try {
      input = JSON.parse(await readBody(req, 256000));
      if (!validView(input) || typeof input.revision !== "string")
        throw new Error();
    } catch {
      reply(400, { error: "Invalid account arrangement." });
      return true;
    }
    const save = async () => {
      const store = await load();
      if (input.revision !== revision(get(store, company))) {
        reply(409, {
          error: "Account arrangement changed elsewhere. Reload to try again.",
        });
        return;
      }
      const view = { order: input.order, hidden: input.hidden };
      store.views = { ...store.views, [company]: view };
      await mkdir(dirname(file), { recursive: true, mode: 0o700 });
      const temp = file + "." + randomBytes(8).toString("hex") + ".tmp";
      try {
        await writeFile(temp, JSON.stringify(store) + "\n", {
          mode: 0o600,
          flag: "wx",
        });
        await rename(temp, file);
      } finally {
        await unlink(temp).catch(() => {});
      }
      reply(200, { ...view, revision: revision(view) });
    };
    queue = queue.catch(() => {}).then(save);
    await queue;
    return true;
  };
}
