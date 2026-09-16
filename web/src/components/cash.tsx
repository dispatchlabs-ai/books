import {
  useState,
  useId,
  useRef,
  useEffect,
  useCallback,
  type ReactNode,
} from "react";
import {
  useBudget,
  decimalMinor,
  decimalValue,
  type BudgetPlan,
} from "@/lib/budget";
import { BudgetAssignments } from "@/components/budgets";
import { Input } from "@/components/ui/input";
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
  scale,
  type Snapshot,
} from "@/lib/books";
import type { ForecastState } from "@/lib/use-forecast";
import { DragDropProvider } from "@dnd-kit/react";
import { move } from "@dnd-kit/helpers";
import { Eye } from "lucide-react";
import { CashAccountRow } from "@/components/cash-account-row";
import { ExpectedIncome } from "@/components/expected-income";
import { orderedAccounts, reorderedView, useCashView } from "@/lib/cash-view";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";

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
  viewScope,
  sharedView,
  currency,
  forecast,
  snapshot,
  ledgerError,
  retryLedger,
  onBudgetEditingChange,
}: {
  company: string;
  viewScope: string;
  sharedView: boolean;
  currency: string;
  forecast: ForecastState;
  snapshot?: Snapshot;
  ledgerError: string;
  retryLedger: () => void;
  onBudgetEditingChange: (editing: boolean) => void;
}) {
  const [horizon, setHorizon] = useState<Horizon>("month");
  const [draft, setDraft] = useState<BudgetPlan>();
  const [amounts, setAmounts] = useState<Record<string, string>>({});
  const [saveError, setSaveError] = useState("");
  const [invalidAccount, setInvalidAccount] = useState("");
  const [saving, setSaving] = useState(false);
  const inputRefs = useRef(new Map<string, HTMLInputElement>());
  const budgetButtonRefs = useRef(new Map<string, HTMLButtonElement>());
  const editOrigin = useRef("");
  const headingRef = useRef<HTMLDivElement>(null);
  const tableRef = useRef<HTMLTableElement>(null);
  const hiddenTriggerRef = useRef<HTMLButtonElement>(null);
  const wasEditing = useRef(false);
  const editing = !!draft;
  const saveErrorRef = useRef<HTMLParagraphElement>(null);
  const errorId = useId();
  const focusEditorElement = useCallback(
    (element: HTMLElement | null | undefined) => {
      if (!element) return;
      element.focus({ preventScroll: true });
      element.style.scrollMarginTop = `${(headingRef.current?.getBoundingClientRect().height ?? 0) + 16}px`;
      element.style.scrollMarginBottom = "16px";
      element.scrollIntoView({ block: "nearest", behavior: "instant" });
    },
    [],
  );
  useEffect(() => {
    if (editing) {
      const input = inputRefs.current.get(editOrigin.current);
      input?.focus({ preventScroll: true });
      input?.select();
    } else if (wasEditing.current) {
      budgetButtonRefs.current
        .get(editOrigin.current)
        ?.focus({ preventScroll: true });
    }
    wasEditing.current = editing;
  }, [editing]);
  useEffect(() => {
    if (saveError && !invalidAccount) focusEditorElement(saveErrorRef.current);
  }, [saveError, invalidAccount, focusEditorElement]);
  const now = today();
  const budgets = useBudget(company, snapshot?.fetchedAt);
  const accountView = useCashView(company, sharedView, viewScope);
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
  const orderedRows = orderedAccounts(rows, accountView.view.order);
  const hiddenRows = orderedRows.filter((r) =>
    accountView.view.hidden.includes(r.code),
  );
  const [dragRows, setDragRows] = useState<CashRow[]>();
  const visibleRows =
    dragRows ??
    orderedRows.filter((r) => !accountView.view.hidden.includes(r.code));
  const layoutDisabled = editing || accountView.saving || !accountView.ready;
  const reorderRows = (codes: string[]) =>
    accountView.update(
      reorderedView(
        accountView.view,
        orderedRows.map((r) => r.code),
        codes,
      ),
    );
  const focusRow = (code?: string, reveal = false) => {
    const row = Array.from(
      tableRef.current?.querySelectorAll("tbody tr") ?? [],
    ).find((el) => el.getAttribute("data-testid") === `cash-row-${code}`);
    const target =
      row?.querySelector<HTMLButtonElement>("button") ??
      hiddenTriggerRef.current;
    if (reveal) focusEditorElement(target);
    else target?.focus({ preventScroll: true });
  };
  const bankOptions = [
    ...new Map(
      [...rows, ...banks].map((b) => [b.code, { code: b.code, name: b.name }]),
    ).values(),
  ];
  // Bank rows can arrive after the budget response or after editing begins.
  // Every displayed input must belong to the draft that will be saved.
  const currentDraft = draft && {
    ...draft,
    buckets: [
      ...draft.buckets,
      ...bankOptions
        .filter((bank) => !draft.buckets.some((b) => b.account === bank.code))
        .map((bank) => ({
          account: bank.code,
          monthly: null,
          expense_accounts: [],
        })),
    ],
  };
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
  const beginEditing = (account: string) => {
    if (!budgets.data?.can_edit) return;
    editOrigin.current = account;
    const plan = structuredClone(budgets.data.plan);
    for (const bank of bankOptions) {
      if (!plan.buckets.some((b) => b.account === bank.code))
        plan.buckets.push({
          account: bank.code,
          monthly: null,
          expense_accounts: [],
        });
    }
    setAmounts(
      Object.fromEntries(
        plan.buckets.map((b) => [
          b.account,
          decimalValue(b.monthly, scale(currency)),
        ]),
      ),
    );
    setDraft(plan);
    setSaveError("");
    setInvalidAccount("");
    onBudgetEditingChange(true);
  };
  const stopEditing = () => {
    setDraft(undefined);
    setSaveError("");
    setInvalidAccount("");
    onBudgetEditingChange(false);
  };
  const saveBudgets = async () => {
    if (!currentDraft || saving) return;
    setSaveError("");
    setInvalidAccount("");
    try {
      const plan = {
        ...currentDraft,
        buckets: currentDraft.buckets.map((b) => {
          try {
            return {
              ...b,
              monthly: decimalMinor(amounts[b.account] ?? "", scale(currency)),
            };
          } catch (e) {
            setInvalidAccount(b.account);
            focusEditorElement(inputRefs.current.get(b.account));
            throw e;
          }
        }),
      };
      setSaving(true);
      await budgets.save(plan);
      stopEditing();
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : "Could not save budgets.");
    } finally {
      setSaving(false);
    }
  };
  return (
    <section aria-label="Cash" data-editing={!!draft}>
      <Tabs
        value={horizon}
        onValueChange={(v) => setHorizon(v as Horizon)}
        className="gap-0"
      >
        <div ref={headingRef} className="cash-heading">
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
            {draft && (
              <div className="budget-actions" aria-label="Budget actions">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={saving}
                  onClick={stopEditing}
                >
                  Cancel
                </Button>
                <Button size="sm" disabled={saving} onClick={saveBudgets}>
                  {saving ? "Saving…" : "Save budgets"}
                </Button>
              </div>
            )}
          </div>
        </div>
        <ExpectedIncome
          forecast={forecast}
          horizon={horizon}
          today={now}
          currency={currency}
        />
        {saveError && (
          <p
            ref={saveErrorRef}
            tabIndex={-1}
            role="alert"
            id={errorId}
            className="cash-notice cash-negative"
          >
            {saveError}
          </p>
        )}
        {accountView.error && (
          <p role="alert" className="cash-notice">
            {accountView.error}
            <Button
              variant="outline"
              size="sm"
              disabled={editing || accountView.saving}
              onClick={accountView.reload}
            >
              Reload arrangement
            </Button>
          </p>
        )}
        {hiddenRows.length > 0 && (
          <div className="cash-hidden-accounts">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  ref={hiddenTriggerRef}
                  variant="ghost"
                  size="sm"
                  disabled={layoutDisabled}
                >
                  <Eye />
                  Hidden accounts ({hiddenRows.length})
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                align="start"
                className="w-72 max-w-[calc(100vw-40px)]"
              >
                {hiddenRows.map((row) => (
                  <DropdownMenuItem
                    key={row.code}
                    onSelect={async () => {
                      const saved = await accountView.update({
                        ...accountView.view,
                        hidden: accountView.view.hidden.filter(
                          (code) => code !== row.code,
                        ),
                      });
                      if (saved)
                        requestAnimationFrame(() => focusRow(row.code, true));
                    }}
                  >
                    <span className="whitespace-normal">Show {row.name}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
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
            <DragDropProvider
              onDragStart={() => setDragRows(visibleRows)}
              onDragEnd={(event) => {
                setDragRows(undefined);
                if (event.canceled || layoutDisabled) return;
                const codes = visibleRows.map((row) => row.code);
                const next = move(codes, event);
                if (next.some((code, i) => code !== codes[i]))
                  void reorderRows(next);
              }}
            >
              <Table
                ref={tableRef}
                className="cash-table"
                aria-label="Bank accounts"
              >
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
                  {visibleRows.map((row, index) => {
                    const p = row.projection,
                      b = budgetRows.get(row.code);
                    return (
                      <CashAccountRow
                        key={row.code}
                        code={row.code}
                        name={row.name}
                        reserved={row.reserved}
                        index={index}
                        count={visibleRows.length}
                        disabled={layoutDisabled}
                        shortfall={!!p?.first}
                        onHide={async () => {
                          const saved = await accountView.update({
                            ...accountView.view,
                            hidden: [...accountView.view.hidden, row.code],
                          });
                          if (saved)
                            requestAnimationFrame(() =>
                              focusRow(
                                visibleRows[index + 1]?.code ??
                                  visibleRows[index - 1]?.code,
                              ),
                            );
                        }}
                        onMove={(delta) => {
                          const codes = visibleRows.map((r) => r.code);
                          const target = index + delta;
                          if (target < 0 || target >= codes.length) return;
                          [codes[index], codes[target]] = [
                            codes[target],
                            codes[index],
                          ];
                          void reorderRows(codes);
                        }}
                      >
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
                          {draft ? (
                            <Input
                              ref={(node) => {
                                if (node) inputRefs.current.set(row.code, node);
                                else inputRefs.current.delete(row.code);
                              }}
                              aria-label={`Monthly budget for ${row.name}`}
                              aria-invalid={
                                invalidAccount === row.code || undefined
                              }
                              aria-describedby={
                                invalidAccount === row.code
                                  ? errorId
                                  : undefined
                              }
                              className="budget-amount-input"
                              inputMode="decimal"
                              placeholder="Not set"
                              value={amounts[row.code] ?? ""}
                              disabled={saving}
                              onChange={(e) =>
                                setAmounts((current) => ({
                                  ...current,
                                  [row.code]: e.target.value,
                                }))
                              }
                            />
                          ) : budgets.data?.can_edit ? (
                            <Button
                              ref={(node) => {
                                if (node)
                                  budgetButtonRefs.current.set(row.code, node);
                                else budgetButtonRefs.current.delete(row.code);
                              }}
                              variant="ghost"
                              className="budget-amount-trigger"
                              aria-label={`Edit monthly budget for ${row.name}, ${b?.monthly != null ? money(b.monthly, currency) : "not set"}`}
                              disabled={
                                accountView.saving ||
                                (!accountView.ready && !accountView.error) ||
                                !!dragRows
                              }
                              onClick={() => beginEditing(row.code)}
                            >
                              {b?.monthly != null ? (
                                money(b.monthly, currency)
                              ) : (
                                <Missing>Not set</Missing>
                              )}
                            </Button>
                          ) : b?.monthly != null ? (
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
                            <Missing>
                              {p ? "Incomplete" : "No forecast"}
                            </Missing>
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
                      </CashAccountRow>
                    );
                  })}
                </TableBody>
              </Table>
            </DragDropProvider>
            {rows.length > 0 && visibleRows.length === 0 && (
              <p className="cash-notice">All accounts are hidden.</p>
            )}
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
      {currentDraft && budgets.data && (
        <div className="budget-assignments">
          <BudgetAssignments
            data={budgets.data}
            currency={currency}
            draft={currentDraft}
            onChange={setDraft}
            saving={saving}
          />
        </div>
      )}
    </section>
  );
}
