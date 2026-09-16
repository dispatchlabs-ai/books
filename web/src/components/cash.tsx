import { useState, useId, type ReactNode } from "react";
import { useBudget } from "@/lib/budget";
import { BudgetEditor } from "@/components/budgets";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { cashWindow, horizons, type Horizon } from "@/lib/cash";
import {
  dateLabel,
  money,
  rangeLabel,
  today,
  type Snapshot,
} from "@/lib/books";
import type { ForecastState } from "@/lib/use-forecast";

function Sparkline({ points }: { points: { date: string; value: bigint }[] }) {
  const id = useId();
  if (!points.length) return <span className="text-muted-foreground">—</span>;
  const values = points.map((p) => p.value);
  const min = values.reduce((a, b) => (a < b ? a : b), 0n),
    max = values.reduce((a, b) => (a > b ? a : b), 0n);
  const span = max - min || 1n;
  const y = (v: bigint) => 4 + Number(((max - v) * 3200n) / span) / 100;
  const zero = y(0n);
  const path = points
    .map(
      (p, i) =>
        `${i ? "H" : "M"}${((i * 300) / Math.max(1, points.length - 1)).toFixed(2)}${i ? "V" : ","}${y(p.value)}`,
    )
    .join(" ");
  return (
    <svg viewBox="0 0 320 40" className="cash-sparkline" aria-hidden="true">
      <defs>
        <clipPath id={id}>
          <rect x="0" y={zero} width="301" height={40 - zero} />
        </clipPath>
      </defs>
      <path
        d={`M0,${zero} H301`}
        stroke="#a1a1aa"
        strokeDasharray="2 3"
        strokeWidth="0.5"
        vectorEffect="non-scaling-stroke"
        fill="none"
      />
      <path
        d={path}
        stroke="#334155"
        strokeWidth="1.2"
        vectorEffect="non-scaling-stroke"
        fill="none"
      />
      <path
        d={path}
        stroke="#dc2626"
        strokeWidth="1.2"
        vectorEffect="non-scaling-stroke"
        fill="none"
        clipPath={`url(#${id})`}
      />
      <text
        x="305"
        y={Math.max(10, Math.min(36, zero + 3))}
        fontSize="8"
        fill="#71717a"
      >
        0
      </text>
    </svg>
  );
}

type CashRow = {
  code: string;
  name: string;
  balance?: string;
  reserved?: boolean;
  projection?: ReturnType<typeof cashWindow>["rows"][number];
};
const Missing = ({ children = "—" }: { children?: ReactNode }) => (
  <span className="text-muted-foreground font-normal">{children}</span>
);

export function Cash({
  company,
  currency,
  forecast,
  snapshot,
  ledgerError,
  retryLedger,
}: {
  company: string;
  currency: string;
  forecast: ForecastState;
  snapshot?: Snapshot;
  ledgerError: string;
  retryLedger: () => void;
}) {
  const [horizon, setHorizon] = useState<Horizon>("month");
  const now = today();
  const budgets = useBudget(company, snapshot?.fetchedAt);
  const data = forecast.data;
  const result = data ? cashWindow(data, horizon, now) : undefined;
  const banks = snapshot?.accounts.filter((a) => a.kind === "BANK") ?? [];
  const planned = new Set(result?.rows.map((r) => r.account.code));
  const rows: CashRow[] = [
    ...(result?.rows.map((r) => ({
      code: r.account.code,
      name: r.account.name,
      reserved: r.account.reserved,
      balance: banks.find((b) => b.code === r.account.code)?.balance,
      projection: r,
    })) ?? []),
    ...banks
      .filter((b) => !planned.has(b.code))
      .map((b) => ({ code: b.code, name: b.name, balance: b.balance })),
  ];
  const budgetRows = new Map(budgets.data?.rows.map((r) => [r.account, r]));
  const bankOptions = [
    ...new Map(
      [...rows, ...banks].map((b) => [b.code, { code: b.code, name: b.name }]),
    ).values(),
  ];
  const incomplete = result?.rows.some((r) => !r.complete);
  const period = result ? rangeLabel(now, result.through) : "";
  const averagePeriod = budgets.data?.months
    .map((m) =>
      new Date(m + "-15T12:00:00").toLocaleDateString("en-US", {
        month: "short",
        year: "numeric",
      }),
    )
    .join(" – ");
  return (
    <section aria-label="Cash">
      <Tabs
        value={horizon}
        onValueChange={(v) => setHorizon(v as Horizon)}
        className="gap-0"
      >
        <div className="cash-heading">
          <div>
            <h1>Cash</h1>
            <p className="cash-period">
              {period || "Bank accounts"}
              <span>{currency}</span>
            </p>
          </div>
          <div className="cash-controls">
            <TabsList aria-label="Cash period" className="cash-tabs">
              {horizons.map((h) => (
                <TabsTrigger key={h} value={h}>
                  {h[0].toUpperCase() + h.slice(1)}
                </TabsTrigger>
              ))}
            </TabsList>
            {budgets.data?.can_edit && (
              <BudgetEditor
                data={budgets.data}
                currency={currency}
                banks={bankOptions}
                save={budgets.save}
              />
            )}
          </div>
        </div>
        {horizons.map((h) => (
          <TabsContent key={h} value={h}>
            {ledgerError && (
              <div role="alert" className="cash-notice">
                Bank balances unavailable: {ledgerError}
                <Button variant="outline" size="sm" onClick={retryLedger}>
                  Retry balances
                </Button>
              </div>
            )}
            {forecast.error && (
              <div role="alert" className="cash-notice">
                Forecast unavailable: {forecast.error}
                <Button variant="outline" size="sm" onClick={forecast.retry}>
                  Try again
                </Button>
              </div>
            )}
            {forecast.scenario && forecast.loading && (
              <p role="status" className="cash-notice">
                Loading cash forecast…
              </p>
            )}
            {incomplete && (
              <p role="status" className="cash-notice">
                Forecast incomplete for this period. Plan ends{" "}
                {dateLabel(data!.plan.through)},{" "}
                {data!.plan.through.slice(0, 4)}. Lowest balances use available
                dates only.
              </p>
            )}
            {!forecast.scenario && (
              <p className="cash-notice">
                No forecast for this entity. Future shortfalls are unknown.
              </p>
            )}
            <Table className="cash-table" aria-label="Bank accounts">
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">Account</TableHead>
                  <TableHead scope="col">Cash now</TableHead>
                  <TableHead scope="col">
                    2-month avg<span>/ month</span>
                  </TableHead>
                  <TableHead scope="col">
                    Budget<span>/ month</span>
                  </TableHead>
                  <TableHead scope="col">Trend</TableHead>
                  <TableHead scope="col">First below zero</TableHead>
                  <TableHead scope="col">Lowest balance</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((row) => {
                  const p = row.projection,
                    b = budgetRows.get(row.code);
                  return (
                    <TableRow
                      key={row.code}
                      data-shortfall={!!p?.first}
                      data-testid={`cash-row-${row.code}`}
                    >
                      <TableCell className="cash-account" data-label="Account">
                        <span className="account-name">{row.name}</span>
                        {row.reserved && (
                          <span className="account-meta">Reserve</span>
                        )}
                      </TableCell>
                      <TableCell
                        data-label="Cash now"
                        className={`cash-current amount ${row.balance !== undefined && BigInt(row.balance) < 0n ? "cash-negative" : ""}`}
                      >
                        {ledgerError || row.balance === undefined ? (
                          <Missing>Unavailable</Missing>
                        ) : (
                          money(row.balance, currency)
                        )}
                      </TableCell>
                      <TableCell
                        data-label="2-month avg / mo"
                        className="amount"
                      >
                        {b ? (
                          money(b.average, currency)
                        ) : (
                          <Missing>
                            {budgets.error
                              ? "Unavailable"
                              : budgets.data
                                ? "Not assigned"
                                : "—"}
                          </Missing>
                        )}
                      </TableCell>
                      <TableCell data-label="Budget / mo" className="amount">
                        {b?.monthly != null ? (
                          money(b.monthly, currency)
                        ) : (
                          <Missing>
                            {budgets.error
                              ? "Unavailable"
                              : budgets.data
                                ? "Not set"
                                : "—"}
                          </Missing>
                        )}
                      </TableCell>
                      <TableCell className="cash-trend">
                        {p ? <Sparkline points={p.points} /> : <Missing />}
                      </TableCell>
                      <TableCell
                        data-label="First below zero"
                        className={p?.first ? "cash-negative" : "cash-status"}
                      >
                        {p?.first ? (
                          <span className="shortfall-date">
                            <span aria-hidden="true" />
                            {dateLabel(p.first)}
                          </span>
                        ) : p?.complete ? (
                          <>
                            <span aria-hidden="true">—</span>
                            <span className="sr-only">
                              No shortfall in this period
                            </span>
                          </>
                        ) : (
                          <Missing>{p ? "Incomplete" : "No forecast"}</Missing>
                        )}
                      </TableCell>
                      <TableCell
                        data-label="Lowest balance"
                        className={`amount ${p?.low !== undefined && p.low < 0n ? "cash-negative" : ""}`}
                      >
                        {p?.low === undefined ? (
                          <Missing />
                        ) : (
                          <>
                            {money(p.low, currency)}
                            {!p.complete && (
                              <span
                                className="partial-marker"
                                aria-label="Available dates only"
                              >
                                *
                              </span>
                            )}
                          </>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
            {!rows.length && (
              <p role="status" className="py-12 text-sm text-muted-foreground">
                {!snapshot && !ledgerError
                  ? "Loading bank accounts…"
                  : "No bank accounts available."}
              </p>
            )}
            <div className="cash-footnotes">
              <p>
                Cash now: posted balance
                {snapshot ? ` as of ${dateLabel(snapshot.to)}` : ""}.
                {data &&
                  ` Forecast: ${forecast.shown} · ${dateLabel(data.plan.as_of)} opening snapshot · End-of-day balances.`}
              </p>
              {budgets.data ? (
                <p>
                  Average monthly spending: {averagePeriod}.
                  {budgets.data.unassigned.count > 0 && (
                    <>
                      {" "}
                      <span className="unassigned">
                        {money(budgets.data.unassigned.average, currency)} /
                        month unassigned.
                      </span>
                    </>
                  )}
                </p>
              ) : budgets.error ? (
                <p role="alert">
                  Budgets unavailable.{" "}
                  <Button variant="link" onClick={budgets.retry}>
                    Retry budgets
                  </Button>
                </p>
              ) : company ? (
                <p role="status">Loading budgets…</p>
              ) : null}
            </div>
          </TabsContent>
        ))}
      </Tabs>
    </section>
  );
}
