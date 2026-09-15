import { useBudget } from "@/lib/budget";
import { BudgetEditor } from "@/components/budgets";
import { useState, useId } from "react";
import { AlertCircle, CheckCircle2, CircleHelp } from "lucide-react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
  if (!points.length) return <div className="h-10" />;
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
    <svg
      viewBox="0 0 320 40"
      className="mt-2 h-10 w-full max-w-80"
      aria-hidden="true"
    >
      <defs>
        <clipPath id={id}>
          <rect x="0" y={zero} width="301" height={40 - zero} />
        </clipPath>
      </defs>
      <path
        d={`M0,${zero} H301`}
        stroke="#a1a1aa"
        strokeDasharray="3 3"
        fill="none"
      />
      <path d={path} stroke="#334155" strokeWidth="1.5" fill="none" />
      <path
        d={path}
        stroke="#dc2626"
        strokeWidth="1.5"
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
export function Cash({
  company,
  forecast,
  snapshot,
  onAccount,
  ledgerError,
  retryLedger,
}: {
  company: string;
  ledgerError: string;
  retryLedger: () => void;
  forecast: ForecastState;
  snapshot?: Snapshot;
  onAccount: (code: string) => void;
}) {
  const [horizon, setHorizon] = useState<Horizon>("month");
  const now = today();
  const budgets = useBudget(company, snapshot?.fetchedAt);
  const data = forecast.data;
  const result = data ? cashWindow(data, horizon, now) : undefined;
  const banks = snapshot?.accounts.filter((a) => a.kind === "BANK") ?? [];
  return (
    <section
      className="rounded-xl border bg-white p-4 sm:p-7"
      aria-label="Cash"
    >
      <Tabs value={horizon} onValueChange={(v) => setHorizon(v as Horizon)}>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-3xl font-semibold tracking-tight">Cash</h1>
          <TabsList aria-label="Cash period" className="h-10!">
            {horizons.map((h) => (
              <TabsTrigger
                key={h}
                value={h}
                className="transition-none min-w-16 px-4 data-[state=active]:bg-white data-[state=active]:text-foreground data-[state=active]:shadow-sm"
              >
                {h[0].toUpperCase() + h.slice(1)}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <div className="my-4 flex flex-wrap items-center justify-between gap-3 text-sm">
          {budgets.data ? (
            <>
              <span className="text-muted-foreground">
                Monthly averages:{" "}
                {budgets.data.months
                  .map((m) =>
                    new Date(m + "-15T12:00:00").toLocaleDateString("en-US", {
                      month: "short",
                      year: "numeric",
                    }),
                  )
                  .join(" – ")}{" "}
                · Posted spending
                {budgets.data.unassigned.count > 0
                  ? ` · ${money(budgets.data.unassigned.average, snapshot?.company.currency ?? "USD")} / month unassigned`
                  : ""}
              </span>
              {budgets.data.can_edit ? (
                <BudgetEditor
                  data={budgets.data}
                  currency={
                    snapshot?.company.currency ?? data?.plan.currency ?? "USD"
                  }
                  banks={
                    data?.plan.accounts
                      .filter((a) => a.kind === "bank")
                      .map((a) => ({ code: a.code, name: a.name })) ?? banks
                  }
                  save={budgets.save}
                />
              ) : (
                <span className="text-xs text-muted-foreground">
                  Read-only budgets
                </span>
              )}
            </>
          ) : budgets.error ? (
            <span role="alert">
              Budgets unavailable.{" "}
              <Button variant="link" onClick={budgets.retry}>
                Retry budgets
              </Button>
            </span>
          ) : (
            <span role="status">
              {company
                ? "Loading budgets…"
                : "Connect a ledger to manage budgets"}
            </span>
          )}
        </div>
        {horizons.map((h) => (
          <TabsContent key={h} value={h}>
            <p className="mb-6 mt-2 text-sm text-muted-foreground">
              {result
                ? `${rangeLabel(now, result.through)} · Sorted by first shortfall`
                : "Bank accounts"}
            </p>
            {ledgerError && (
              <div role="alert" className="mb-4 rounded-lg border p-4">
                Bank balances unavailable: {ledgerError}{" "}
                <Button variant="outline" onClick={retryLedger}>
                  Retry balances
                </Button>
              </div>
            )}
            {forecast.scenario && forecast.loading && (
              <p role="status" className="py-8">
                Loading cash forecast…
              </p>
            )}
            {forecast.error && (
              <div role="alert" className="mb-4 rounded-lg border p-4">
                Forecast unavailable: {forecast.error}{" "}
                <Button variant="outline" onClick={forecast.retry}>
                  Try again
                </Button>
              </div>
            )}
            {data && result ? (
              <>
                <div className="hidden grid-cols-[minmax(0,2fr)_1fr_1fr_1fr_1.1fr_1fr] gap-5 px-4 pb-3 text-xs text-muted-foreground lg:grid">
                  <span>Bank account</span>
                  <span>Cash now</span>
                  <span>2-month avg / mo</span>
                  <span>Budget / mo</span>
                  <span>First below zero</span>
                  <span className="text-right">Lowest balance</span>
                </div>
                <div className="space-y-2">
                  {result.rows.map((row) => {
                    const ledger = snapshot?.accounts.find(
                      (a) => a.code === row.account.code,
                    );
                    const negative = !!row.first;
                    const Icon = negative
                      ? AlertCircle
                      : row.complete
                        ? CheckCircle2
                        : CircleHelp;
                    const tone = negative
                      ? "text-red-700"
                      : row.complete
                        ? "text-teal-700"
                        : "text-muted-foreground";
                    return (
                      <button
                        key={row.account.code}
                        onClick={() => onAccount(row.account.code)}
                        className={`grid w-full grid-cols-2 items-start gap-4 rounded-lg border p-4 text-left transition hover:bg-zinc-50 lg:grid-cols-[minmax(0,2fr)_1fr_1fr_1fr_1.1fr_1fr] lg:gap-5 ${negative ? "border-red-200 bg-red-50/40" : ""}`}
                      >
                        <div className="col-span-2 flex min-w-0 gap-3 lg:col-span-1">
                          <Icon className={`mt-0.5 size-5 shrink-0 ${tone}`} />
                          <div className="min-w-0 flex-1">
                            <p className="font-medium break-words">
                              {row.account.name}
                            </p>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {row.account.code}
                              {row.account.reserved ? " · Reserve" : ""}
                            </p>
                            <Sparkline points={row.points} />
                          </div>
                        </div>
                        <div>
                          <span className="mb-1 block text-xs text-muted-foreground lg:hidden">
                            Cash now
                          </span>
                          <span className="amount font-medium">
                            {ledger
                              ? money(ledger.balance, data.plan.currency)
                              : "Unavailable"}
                          </span>
                        </div>
                        <div>
                          <span className="mb-1 block text-xs text-muted-foreground lg:hidden">
                            2-month avg / mo
                          </span>
                          <span className="amount font-medium">
                            {budgets.data?.rows.find(
                              (b) => b.account === row.account.code,
                            )
                              ? money(
                                  budgets.data.rows.find(
                                    (b) => b.account === row.account.code,
                                  )!.average,
                                  data.plan.currency,
                                )
                              : "Not assigned"}
                          </span>
                        </div>
                        <div>
                          <span className="mb-1 block text-xs text-muted-foreground lg:hidden">
                            Budget / mo
                          </span>
                          <span className="amount font-medium">
                            {budgets.data?.rows.find(
                              (b) => b.account === row.account.code,
                            )?.monthly != null
                              ? money(
                                  budgets.data.rows.find(
                                    (b) => b.account === row.account.code,
                                  )!.monthly!,
                                  data.plan.currency,
                                )
                              : "Not set"}
                          </span>
                        </div>
                        <div className={`text-sm ${tone}`}>
                          <span className="mb-1 block text-xs text-muted-foreground lg:hidden">
                            First below zero
                          </span>
                          {row.first
                            ? dateLabel(row.first)
                            : row.complete
                              ? "Stays at or above zero"
                              : "Forecast incomplete"}
                        </div>
                        <div
                          className={`col-span-2 text-sm lg:col-span-1 lg:text-right ${tone}`}
                        >
                          <span className="mb-1 block text-xs text-muted-foreground lg:hidden">
                            Lowest balance
                          </span>
                          <span className="amount font-medium">
                            {row.low === undefined
                              ? "Unavailable"
                              : money(row.low, data.plan.currency)}
                          </span>
                          {!row.complete && row.low !== undefined && (
                            <span className="mt-1 block text-xs text-muted-foreground">
                              Available dates only
                            </span>
                          )}
                        </div>
                      </button>
                    );
                  })}
                </div>
                {!result.rows.length && <p>No bank accounts in this plan.</p>}
                {banks
                  .filter(
                    (a) => !data.plan.accounts.some((p) => p.code === a.code),
                  )
                  .map((a) => (
                    <button
                      key={a.code}
                      onClick={() => onAccount(a.code)}
                      className="mt-2 flex w-full flex-wrap justify-between gap-4 rounded-lg border p-4 text-left"
                    >
                      <span>{a.name}</span>
                      <span className="amount">
                        {money(a.balance, data.plan.currency)}
                      </span>
                      <span className="text-sm text-muted-foreground">
                        Not included in forecast
                      </span>
                      <span className="text-sm">
                        2-month avg / mo:{" "}
                        {budgets.data?.rows.find((b) => b.account === a.code)
                          ? money(
                              budgets.data.rows.find(
                                (b) => b.account === a.code,
                              )!.average,
                              data.plan.currency,
                            )
                          : "Not assigned"}{" "}
                        · Budget / mo:{" "}
                        {budgets.data?.rows.find((b) => b.account === a.code)
                          ?.monthly != null
                          ? money(
                              budgets.data.rows.find(
                                (b) => b.account === a.code,
                              )!.monthly!,
                              data.plan.currency,
                            )
                          : "Not set"}
                      </span>
                    </button>
                  ))}
                <p className="mt-5 text-xs leading-5 text-muted-foreground">
                  Projected end-of-day balances · {forecast.shown} plan ·
                  Opening snapshot {dateLabel(data.plan.as_of)}. Cash now is the
                  posted ledger balance
                  {snapshot ? ` as of ${dateLabel(snapshot.to)}` : ""}.
                </p>
                {result.rows.some((r) => !r.complete) && (
                  <p role="status" className="mt-2 text-sm text-amber-800">
                    The forecast does not cover this entire period. Plan ends{" "}
                    {dateLabel(data.plan.through)},{" "}
                    {data.plan.through.slice(0, 4)}.
                  </p>
                )}
              </>
            ) : (
              !forecast.scenario && (
                <>
                  <p className="mb-5 text-sm text-muted-foreground">
                    No cash forecast is configured for this entity. Future
                    shortfalls are unknown.
                  </p>
                  {banks.map((a) => (
                    <button
                      key={a.id}
                      onClick={() => onAccount(a.code)}
                      className="mb-2 flex w-full flex-wrap justify-between gap-4 rounded-lg border p-4 text-left"
                    >
                      <span>{a.name}</span>
                      <span className="amount">
                        {money(a.balance, snapshot!.company.currency)}
                      </span>
                      <span className="text-sm">
                        2-month avg / mo:{" "}
                        {budgets.data?.rows.find((b) => b.account === a.code)
                          ? money(
                              budgets.data.rows.find(
                                (b) => b.account === a.code,
                              )!.average,
                              snapshot!.company.currency,
                            )
                          : "Not assigned"}{" "}
                        · Budget / mo:{" "}
                        {budgets.data?.rows.find((b) => b.account === a.code)
                          ?.monthly != null
                          ? money(
                              budgets.data.rows.find(
                                (b) => b.account === a.code,
                              )!.monthly!,
                              snapshot!.company.currency,
                            )
                          : "Not set"}
                      </span>
                    </button>
                  ))}
                  {!snapshot && !ledgerError && (
                    <p role="status">Loading bank accounts…</p>
                  )}
                </>
              )
            )}
          </TabsContent>
        ))}
      </Tabs>
    </section>
  );
}
