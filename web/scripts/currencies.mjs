import { format } from "prettier";
import { readFile, writeFile } from "node:fs/promises";
const source = await readFile(
  new URL("../../internal/money/currencies.go", import.meta.url),
  "utf8",
);
const entries = [...source.matchAll(/"([A-Z]{3})":\s*\{"\d+",\s*(\d+)\}/g)].map(
  (m) => [m[1], Number(m[2])],
);
if (entries.length < 100)
  throw new Error("Currency registry could not be parsed");
const output = await format(
  "// Generated from internal/money/currencies.go. Run npm run currencies.\nexport const currencyScales: Record<string, number> = " +
    JSON.stringify(Object.fromEntries(entries), null, 2) +
    "\n",
  { parser: "typescript" },
);
const file = new URL("../src/lib/currencies.ts", import.meta.url);
if (process.argv.includes("--check")) {
  if ((await readFile(file, "utf8")) !== output)
    throw new Error("Currency metadata is stale; run npm run currencies");
} else await writeFile(file, output);
