import { readFile, writeFile, rename } from "node:fs/promises";
import { createHash, randomBytes } from "node:crypto";
import { readBody } from "./session-auth.mjs";
export function createFeedback(config) {
  let queue = Promise.resolve();
  const revision = (data) =>
    createHash("sha256").update(JSON.stringify(data)).digest("hex");
  const loadAnswers = async () => {
    try {
      return await load(config.answers);
    } catch (e) {
      if (e.code !== "ENOENT") throw e;
      return {};
    }
  };
  const load = async (path) => JSON.parse(await readFile(path, "utf8"));
  return async (req, res, url) => {
    const reply = (status, body) => {
      res.writeHead(status, {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      });
      res.end(JSON.stringify(body));
    };
    if (url.pathname === "/feedback/" && req.method === "GET" && config.page) {
      // Optional operator-owned page preserves existing annotations. Only its exact
      // inline scripts are allowed; request content never enters this template.
      const html = await readFile(config.page, "utf8");
      const hashes = [
        ...html.matchAll(/<script(?:\s[^>]*)?>([\s\S]*?)<\/script>/gi),
      ].map(
        (match) =>
          "'sha256-" +
          createHash("sha256").update(match[1]).digest("base64") +
          "'",
      );
      res.setHeader(
        "Content-Security-Policy",
        "default-src 'self'; script-src 'self' " +
          hashes.join(" ") +
          "; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
      );
      res.writeHead(200, {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
      });
      res.end(html);
      return true;
    }
    if (url.pathname === "/feedback/api/items" && req.method === "GET") {
      const { records } = await load(config.sources);
      const answers = await loadAnswers();
      res.setHeader("ETag", revision(answers));
      reply(200, { items: records.map((r) => r.source), answers });
      return true;
    }
    if (url.pathname === "/feedback/api/answers" && req.method === "POST") {
      let data;
      try {
        data = JSON.parse(await readBody(req, 100000));
      } catch {
        reply(400, { error: "Invalid answers." });
        return true;
      }
      const valid = new Set(
        (await load(config.snapshot)).transactions.map((r) => r.source_uid),
      );
      if (
        !data ||
        Array.isArray(data) ||
        typeof data !== "object" ||
        Object.entries(data).some(
          ([key, value]) =>
            !valid.has(key) || typeof value !== "string" || value.length > 4000,
        )
      ) {
        reply(400, { error: "Invalid answers." });
        return true;
      }
      const save = async () => {
        const latest = await loadAnswers();
        if (req.headers["if-match"] !== revision(latest)) {
          reply(409, {
            error: "Notes changed in another tab. Reload before saving.",
          });
          return false;
        }
        data = { ...latest, ...data };
        const temp =
          config.answers + "." + randomBytes(8).toString("hex") + ".tmp";
        await writeFile(temp, JSON.stringify(data, null, 2) + "\n", {
          mode: 0o600,
          flag: "wx",
        });
        await rename(temp, config.answers);
        res.setHeader("ETag", revision(data));
        return true;
      };
      queue = queue.catch(() => {}).then(save);
      if (await queue) reply(200, { ok: true });
      return true;
    }
    return false;
  };
}
