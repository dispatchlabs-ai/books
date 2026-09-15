import { createSessionAuth } from "./session-auth.mjs";
import { createFeedback } from "./feedback.mjs";
import { timingSafeEqual } from "node:crypto";
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, extname } from "node:path";
import { pathToFileURL } from "node:url";

const json = (res, status, data) => {
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
  });
  res.end(JSON.stringify(data));
};
export function createBooksWebServer(config = {}) {
  const {
    publicOrigin,
    proxyToken,
    upstream,
    token,
    agentURL,
    agentToken,
    dist = resolve("dist"),
    fetcher = fetch,
    forecastPlans = {},
    sessionAuth,
    feedback,
  } = config;
  if (Boolean(upstream) !== Boolean(token))
    throw new Error("Set both BOOKS_API_URL and BOOKS_API_TOKEN_FILE.");
  if (Boolean(publicOrigin) !== Boolean(proxyToken))
    throw new Error(
      "Hosted mode requires both a public origin and a proxy token.",
    );
  if (publicOrigin) {
    const origin = new URL(publicOrigin);
    if (
      origin.protocol !== "https:" ||
      origin.origin !== publicOrigin ||
      !upstream ||
      proxyToken.length < 32
    )
      throw new Error(
        "Hosted mode requires an exact HTTPS origin, connected API, and strong proxy token.",
      );
  }
  for (const value of [upstream, agentURL].filter(Boolean)) {
    const url = new URL(value);
    if (
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      (url.protocol !== "https:" &&
        !(
          url.protocol === "http:" &&
          ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)
        ))
    )
      throw new Error(
        "Upstreams require HTTPS or loopback HTTP, without URL credentials or query parameters.",
      );
  }
  if (sessionAuth && !publicOrigin)
    throw new Error("Session login requires hosted HTTPS mode.");
  if (feedback && !sessionAuth)
    throw new Error("Feedback requires session login.");
  const authenticate = sessionAuth ? createSessionAuth(sessionAuth) : null;
  const feedbackHandler = feedback ? createFeedback(feedback) : null;
  return createServer(async (req, res) => {
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader("Referrer-Policy", "same-origin");
    res.setHeader(
      "Content-Security-Policy",
      "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
    );
    try {
      // Hosted traffic must pass through an authenticated, loopback TLS proxy.
      if (publicOrigin) {
        const supplied = Buffer.from(req.headers["x-books-proxy-token"] ?? "");
        const expected = Buffer.from(proxyToken);
        if (
          req.headers.host !== new URL(publicOrigin).host ||
          supplied.length !== expected.length ||
          !timingSafeEqual(supplied, expected)
        )
          return json(res, 403, { error: "Authenticated proxy required." });
        if (req.headers.origin && req.headers.origin !== publicOrigin)
          return json(res, 403, { error: "Origin not allowed." });
      } else {
        if (
          !/^((localhost|127\.0\.0\.1)(:\d+)?|\[::1\](:\d+)?)$/.test(
            req.headers.host ?? "",
          )
        )
          return json(res, 403, { error: "Host not allowed." });
        if (
          req.headers.origin &&
          ![
            `http://${req.headers.host}`,
            "http://127.0.0.1:5173",
            "http://localhost:5173",
          ].includes(req.headers.origin)
        )
          return json(res, 403, { error: "Origin not allowed." });
      }
      const url = new URL(req.url, "http://localhost");
      if (authenticate && (await authenticate(req, res, url, publicOrigin)))
        return;
      if (feedbackHandler && (await feedbackHandler(req, res, url))) return;
      if (url.pathname === "/feedback") {
        res.writeHead(303, { Location: "/feedback/" });
        res.end();
        return;
      }
      if (url.pathname.startsWith("/feedback/") && !feedback)
        return json(res, 404, { error: "Feedback is not configured." });
      if (url.pathname === "/api/config" && req.method === "GET")
        return json(res, 200, {
          ...(sessionAuth ? { session: true } : {}),
          demo: !upstream,
          agent: Boolean(upstream && agentURL),
          // Scenario names only; operator file paths never leave the server.
          forecasts: upstream
            ? Object.fromEntries(
                Object.entries(forecastPlans).map(([company, plans]) => [
                  company,
                  Object.keys(plans),
                ]),
              )
            : {},
        });
      const forecastMatch = url.pathname.match(
        /^\/api\/books\/companies\/([A-Za-z0-9_-]+)\/cash-forecast$/,
      );
      if (forecastMatch && req.method === "GET") {
        const company = forecastMatch[1];
        const scenario = url.searchParams.get("scenario") ?? "baseline";
        const file = forecastPlans[company]?.[scenario];
        if (!upstream || typeof file !== "string")
          return json(res, 404, {
            error: "No cash plan is configured for this entity and scenario.",
          });
        // Paths come only from operator configuration, never from the request.
        const raw = await readFile(file, "utf8");
        if (Buffer.byteLength(raw) > 2_000_000)
          return json(res, 413, {
            error: "Cash plan exceeds the input limit.",
          });
        const reply = await fetcher(
          upstream.replace(/\/$/, "") +
            "/v1/companies/" +
            company +
            "/operations/cash_forecast",
          {
            method: "POST",
            headers: {
              Authorization: `Bearer ${token}`,
              "Content-Type": "application/json",
            },
            body: raw,
            redirect: "error",
            signal: AbortSignal.timeout(15000),
          },
        );
        const body = await reply.json();
        if (!reply.ok || body.schema !== "books.api/v1" || body.ok !== true)
          return json(res, reply.ok ? 502 : reply.status, {
            error: "Cash plan could not be validated for this entity.",
          });
        return json(res, 200, {
          ...body.data,
          scenarios: Object.keys(forecastPlans[company]),
        });
      }
      const allowed =
        /^\/api\/books\/companies(?:\/[A-Za-z0-9_-]+\/reports\/(?:general-ledger|profit-loss))?$/.test(
          url.pathname,
        );
      if (allowed && req.method === "GET") {
        if (!upstream)
          return json(res, 503, { error: "Books API is not connected." });
        const reply = await fetcher(
          upstream.replace(/\/$/, "") +
            "/v1" +
            url.pathname.slice("/api/books".length) +
            url.search,
          {
            headers: { Authorization: `Bearer ${token}` },
            redirect: "error",
            signal: AbortSignal.timeout(15000),
          },
        );
        const body = await reply.json();
        if (!reply.ok || body.schema !== "books.api/v1" || body.ok !== true)
          return json(res, reply.ok ? 502 : reply.status, {
            error:
              "Books could not load this view. Check the connection and permissions.",
          });
        return json(res, 200, body.data);
      }
      if (url.pathname === "/api/ask" && req.method === "POST") {
        if (!upstream || !agentURL)
          return json(res, 503, {
            error: "An AI connection has not been configured yet.",
          });
        let raw = "";
        for await (const chunk of req) {
          raw += chunk;
          if (Buffer.byteLength(raw) > 32768)
            return json(res, 413, { error: "Conversation is too long." });
        }
        let body;
        try {
          body = JSON.parse(raw);
        } catch {
          return json(res, 400, { error: "Invalid request." });
        }
        if (
          !body ||
          typeof body.company !== "string" ||
          !Array.isArray(body.messages) ||
          body.messages.length > 20 ||
          !body.messages.length ||
          body.messages.some(
            (m) =>
              !["user", "assistant"].includes(m.role) ||
              typeof m.content !== "string" ||
              !m.content.trim() ||
              m.content.length > (m.role === "user" ? 8000 : 32000),
          ) ||
          (body.account !== undefined && typeof body.account !== "string")
        )
          return json(res, 400, { error: "Invalid conversation." });
        const companiesResponse = await fetcher(
          upstream.replace(/\/$/, "") + "/v1/companies",
          {
            headers: { Authorization: `Bearer ${token}` },
            redirect: "error",
            signal: AbortSignal.timeout(15000),
          },
        );
        const companies = await companiesResponse.json();
        if (
          !companiesResponse.ok ||
          companies.schema !== "books.api/v1" ||
          !companies.ok ||
          !companies.data.some((c) => c.key === body.company)
        )
          return json(res, 403, { error: "Entity is unavailable." });
        const reply = await fetcher(agentURL, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            ...(agentToken ? { Authorization: `Bearer ${agentToken}` } : {}),
          },
          body: JSON.stringify({
            company: body.company,
            account: body.account,
            messages: body.messages.map((m) => ({
              role: m.role,
              content: m.content,
            })),
          }),
          redirect: "error",
          signal: AbortSignal.timeout(60000),
        });
        if (!reply.ok)
          return json(res, 502, {
            error: "Books could not get an answer. Please try again.",
          });
        const answer = await reply.json();
        if (typeof answer.text !== "string" || answer.text.length > 32000)
          return json(res, 502, {
            error: "The AI connection returned an invalid answer.",
          });
        // Only plain text is accepted; no executable markup or tool calls from the adapter.
        return json(res, 200, { text: answer.text });
      }
      if (url.pathname.startsWith("/api/"))
        return json(res, 404, { error: "Route not found." });
      if (req.method !== "GET" && req.method !== "HEAD")
        return json(res, 405, { error: "Method not allowed." });
      const requested = resolve(
        dist,
        "." +
          decodeURIComponent(
            url.pathname === "/feedback/"
              ? "/feedback/index.html"
              : url.pathname,
          ),
      );
      if (requested !== dist && !requested.startsWith(dist + "/"))
        return json(res, 404, { error: "Not found." });
      let data,
        name = requested;
      try {
        data = await readFile(name);
      } catch {
        name = resolve(dist, "index.html");
        data = await readFile(name);
      }
      const types = {
        ".html": "text/html",
        ".js": "text/javascript",
        ".css": "text/css",
        ".svg": "image/svg+xml",
        ".woff2": "font/woff2",
        ".png": "image/png",
      };
      res.writeHead(200, {
        "Content-Type": types[extname(name)] ?? "application/octet-stream",
        "Cache-Control": "no-store",
      });
      res.end(req.method === "HEAD" ? undefined : data);
    } catch {
      json(res, 502, { error: "Books is unavailable. Please try again." });
    }
  });
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const secret = async (name) =>
    process.env[name]
      ? (await readFile(process.env[name], "utf8")).trim()
      : undefined;
  const server = createBooksWebServer({
    sessionAuth: process.env.BOOKS_WEB_LOGIN_FILE
      ? JSON.parse(await readFile(process.env.BOOKS_WEB_LOGIN_FILE, "utf8"))
      : undefined,
    feedback: process.env.BOOKS_WEB_FEEDBACK_FILE
      ? JSON.parse(await readFile(process.env.BOOKS_WEB_FEEDBACK_FILE, "utf8"))
      : undefined,
    publicOrigin: process.env.BOOKS_WEB_ORIGIN,
    proxyToken: await secret("BOOKS_WEB_PROXY_TOKEN_FILE"),
    upstream: process.env.BOOKS_API_URL,
    token: await secret("BOOKS_API_TOKEN_FILE"),
    agentURL: process.env.BOOKS_AGENT_URL,
    agentToken: await secret("BOOKS_AGENT_TOKEN_FILE"),
    forecastPlans: process.env.BOOKS_FORECAST_PLANS_FILE
      ? JSON.parse(
          await readFile(process.env.BOOKS_FORECAST_PLANS_FILE, "utf8"),
        )
      : {},
  });
  const port = Number(process.env.BOOKS_WEB_PORT ?? 8788);
  server.listen(port, "127.0.0.1", () =>
    console.log(`Books web: http://127.0.0.1:${port}`),
  );
}
