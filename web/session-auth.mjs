import { randomBytes, scrypt, timingSafeEqual, createHash } from "node:crypto";
import { promisify } from "node:util";
const derive = promisify(scrypt);
const cookieName = "__Host-books_session";
const digest = (value) => createHash("sha256").update(value).digest("hex");
export async function passwordRecord(username, password) {
  const salt = randomBytes(32).toString("hex");
  return {
    username,
    salt,
    hash: (await derive(password, salt, 64)).toString("hex"),
  };
}
export async function readBody(req, limit = 8192) {
  let raw = "";
  for await (const chunk of req) {
    raw += chunk;
    if (Buffer.byteLength(raw) > limit) throw new Error("Request too large");
  }
  return raw;
}
const page = (res, message = "", status = 200) => {
  res.writeHead(status, {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-store",
  });
  res.end(
    `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sign in · Books</title><style>body{margin:0;background:#f7f8fa;color:#17202b;font:16px system-ui;display:grid;min-height:100dvh;place-items:center}main{box-sizing:border-box;width:min(400px,calc(100% - 32px));padding:36px;background:white;border:1px solid #e3e6eb;border-radius:18px}h1{font-size:26px;margin:0 0 8px}p{color:#586474;line-height:1.5}label{display:block;margin-top:20px}input,button{box-sizing:border-box;width:100%;padding:12px;font:inherit;border-radius:8px;border:1px solid #bcc4cf;margin-top:8px}button{background:#172b46;color:white;margin-top:26px;cursor:pointer}.error{color:#a22424}</style><main><h1>Sign in to Books</h1><p>Your finances, in one place.</p>${message ? `<p class="error" role="alert">${message}</p>` : ""}<form method="post" action="/login"><label>Username<input name="username" autocomplete="username" required maxlength="128"></label><label>Password<input name="password" type="password" autocomplete="current-password" required maxlength="1024"></label><button>Sign in</button></form></main></html>`,
  );
};
export function createSessionAuth(record, { now = Date.now } = {}) {
  if (
    !record ||
    typeof record.username !== "string" ||
    !record.username ||
    record.username.length > 128 ||
    !/^[a-f0-9]{64}$/.test(record.salt) ||
    !/^[a-f0-9]{128}$/.test(record.hash)
  )
    throw new Error("Invalid web login configuration");
  const sessions = new Map();
  let attempts = [];
  return async (req, res, url, origin) => {
    const time = now();
    for (const [key, expiry] of sessions)
      if (expiry <= time) sessions.delete(key);
    const value =
      (req.headers.cookie ?? "")
        .split(";")
        .map((v) => v.trim())
        .find((v) => v.startsWith(cookieName + "="))
        ?.slice(cookieName.length + 1) ?? "";
    const authenticated = sessions.has(digest(value));
    const redirect = (location) => {
      res.writeHead(303, { Location: location, "Cache-Control": "no-store" });
      res.end();
    };
    if (url.pathname === "/login") {
      if (req.method === "GET") {
        if (authenticated) redirect("/");
        else page(res);
        return true;
      }
      if (
        req.method !== "POST" ||
        req.headers.origin !== origin ||
        req.headers["content-type"]?.split(";")[0] !==
          "application/x-www-form-urlencoded"
      ) {
        page(res, "Please sign in using this form.", 403);
        return true;
      }
      attempts = attempts.filter((t) => time - t < 60000);
      if (attempts.length >= 6) {
        res.setHeader("Retry-After", "60");
        page(res, "Please wait a minute and try again.", 429);
        return true;
      }
      attempts.push(time);
      let form;
      try {
        form = new URLSearchParams(await readBody(req));
      } catch {
        page(res, "Invalid sign-in request.", 400);
        return true;
      }
      const password = form.get("password") ?? "";
      if (password.length > 1024) {
        page(res, "Invalid sign-in request.", 400);
        return true;
      }
      const actual = await derive(password, record.salt, 64);
      if (
        !timingSafeEqual(actual, Buffer.from(record.hash, "hex")) ||
        form.get("username") !== record.username
      ) {
        page(res, "Username or password was incorrect.", 401);
        return true;
      }
      if (value) sessions.delete(digest(value));
      if (sessions.size >= 100) sessions.delete(sessions.keys().next().value);
      const session = randomBytes(32).toString("hex");
      sessions.set(digest(session), time + 12 * 60 * 60 * 1000);
      res.setHeader(
        "Set-Cookie",
        `${cookieName}=${session}; Path=/; Secure; HttpOnly; SameSite=Strict; Max-Age=43200`,
      );
      redirect("/");
      return true;
    }
    if (url.pathname === "/logout" && req.method === "POST") {
      if (req.headers.origin !== origin) {
        page(res, "Origin not allowed.", 403);
        return true;
      }
      sessions.delete(digest(value));
      res.setHeader(
        "Set-Cookie",
        `${cookieName}=; Path=/; Secure; HttpOnly; SameSite=Strict; Max-Age=0`,
      );
      redirect("/login");
      return true;
    }
    if (authenticated) {
      if (
        !["GET", "HEAD"].includes(req.method) &&
        req.headers.origin !== origin
      ) {
        page(res, "Origin not allowed.", 403);
        return true;
      }
      return false;
    }
    if (
      url.pathname.startsWith("/api/") ||
      url.pathname.startsWith("/feedback/api/")
    ) {
      res.writeHead(401, {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      });
      res.end('{"error":"Sign in required."}');
    } else redirect("/login");
    return true;
  };
}
