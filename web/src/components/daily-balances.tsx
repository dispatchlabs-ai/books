import { forwardRef, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceDot,
  ReferenceLine,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { ArrowRightLeft, ChevronDown } from "lucide-react";
import { Amount } from "@/components/amount";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ChartContainer } from "@/components/ui/chart";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { dateLabel, dayLabel, money, rangeLabel, scale } from "@/lib/books";
import type { Forecast, ForecastDay, Outlook } from "@/lib/forecast";

export type DayFilter = "short" | "activity" | "all";
const statusLabel: Record<string, string> = {
  confirmed: "Confirmed",
  estimated: "Estimated",
  proposed: "Proposed",
  actual: "Actual",
};
const PAGE = 31;

export const DailyBalances = forwardRef<
  HTMLHeadingElement,
  {
    forecast: Forecast;
    outlook: Outlook;
    scenario: string;
    account: string;
    onAccountChange: (code: string) => void;
    filter?: DayFilter;
    onFilterChange: (filter: DayFilter | undefined) => void;
  }
>(function DailyBalances(
  {
    forecast,
    outlook,
    scenario,
    account,
    onAccountChange,
    filter,
    onFilterChange,
  },
  heading,
) {
  const currency = forecast.plan.currency;
  const banks = forecast.plan.accounts.filter((a) => a.kind === "bank");
  const names = new Map(forecast.plan.accounts.map((a) => [a.code, a.name]));
  const gaps = new Map(outlook.gaps.map((g) => [g.code, g]));
  const active = banks.find((a) => a.code === account) ?? banks[0];
  // Expanded day and paging belong to one account; another account starts fresh.
  const [view, setView] = useState<{
    account: string;
    selected?: string;
    limit: number;
  }>({ account: "", limit: PAGE });
  // Includes the opening snapshot day, as the engine's lows do.
  const rows = forecast.days.filter((d) => d.account === active?.code);
  if (!active) return null;
  const { selected, limit } =
    view.account === active.code ? view : { selected: undefined, limit: PAGE };
  const setSelected = (date: string | undefined) =>
    setView({ account: active.code, selected: date, limit });
  const setLimit = (value: number) =>
    setView({ account: active.code, selected, limit: value });
  const short = rows.filter((d) => BigInt(d.shortfall) > 0n);
  const activity = rows.filter((d) => d.movements.length > 0);
  const shown: DayFilter =
    filter ?? (short.length ? "short" : activity.length ? "activity" : "all");
  const listed =
    shown === "short" ? short : shown === "activity" ? activity : rows;
  // Nothing chosen opens the first listed day; an empty string means all closed.
  const open =
    selected === ""
      ? undefined
      : (listed.find((d) => d.date === selected)?.date ?? listed[0]?.date);
  const gap = gaps.get(active.code);
  const low = rows.reduce<ForecastDay | undefined>(
    (a, d) => (!a || BigInt(d.closing) < BigInt(a.closing) ? d : a),
    undefined,
  );
  const choose = (code: string) => {
    onAccountChange(code);
    onFilterChange(undefined);
  };
  return (
    <section
      aria-labelledby="daily-balances"
      className="@container mt-10 min-w-0"
    >
      <h2
        id="daily-balances"
        ref={heading}
        tabIndex={-1}
        className="section-title scroll-mt-6 outline-none"
      >
        Daily balances by account
      </h2>
      <p className="mt-1 text-sm text-muted-foreground">
        End-of-day estimates · {scenario} ·{" "}
        {rangeLabel(
          rows[0]?.date ?? forecast.plan.through,
          forecast.plan.through,
        )}
      </p>
      <div className="mt-5 rounded-xl border bg-card p-4 sm:p-6">
        <label className="stat-label block" htmlFor="daily-account">
          Account
        </label>
        <Select value={active.code} onValueChange={choose}>
          <SelectTrigger
            id="daily-account"
            aria-label="Bank account"
            className="mt-2 w-full bg-background data-[size=default]:h-11 @xl:w-96"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper" className="max-h-80">
            {banks.map((a) => (
              <SelectItem key={a.code} value={a.code} className="min-h-10">
                <span className="truncate">{a.name}</span>
                {gaps.has(a.code) && <Badge variant="destructive">Short</Badge>}
                {a.reserved && <Badge variant="outline">Reserve</Badge>}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <dl className="mt-5 grid grid-cols-2 gap-x-4 gap-y-4 @2xl:grid-cols-4 @2xl:divide-x">
          <div className="min-w-0">
            <dt className="stat-label">
              Opening {dateLabel(forecast.plan.as_of)}
            </dt>
            <dd className="mt-1 text-lg font-semibold">
              <Amount value={BigInt(active.opening)} currency={currency} />
            </dd>
          </div>
          <div className="min-w-0 @2xl:pl-4">
            <dt className="stat-label">Lowest</dt>
            <dd className="mt-1 text-lg font-semibold">
              {low ? (
                <>
                  <Amount
                    value={BigInt(low.closing)}
                    currency={currency}
                    className={
                      BigInt(low.shortfall) > 0n ? "text-destructive" : ""
                    }
                  />
                  <span className="ml-1.5 text-xs font-normal text-muted-foreground">
                    {dateLabel(low.date)}
                  </span>
                </>
              ) : (
                "—"
              )}
            </dd>
          </div>
          <div className="min-w-0 @2xl:pl-4">
            <dt className="stat-label">Floor</dt>
            <dd className="mt-1 text-lg font-semibold">
              <Amount value={BigInt(active.floor)} currency={currency} />
            </dd>
          </div>
          <div className="min-w-0 @2xl:pl-4">
            <dt className="stat-label">Shortfall at lowest</dt>
            <dd className="mt-1 text-lg font-semibold">
              <Amount
                value={gap?.shortfall ?? 0n}
                currency={currency}
                tone="shortfall"
              />
              {gap && (
                <span className="block text-xs font-normal text-muted-foreground">
                  {gap.daysShort} {gap.daysShort === 1 ? "day" : "days"} below
                  floor
                </span>
              )}
            </dd>
          </div>
        </dl>
        <p className="mt-4 text-xs leading-5 text-muted-foreground">
          Opening balance: {active.evidence}
        </p>
        <BalanceChart
          rows={rows}
          floor={active.floor}
          low={low && BigInt(low.shortfall) > 0n ? low : undefined}
          currency={currency}
          name={active.name}
        />
      </div>
      <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
        <ToggleGroup
          type="single"
          variant="outline"
          spacing={0}
          value={shown}
          onValueChange={(v) => {
            if (!v) return;
            onFilterChange(v as DayFilter);
            setView({ account: active.code, limit: PAGE });
          }}
          aria-label="Days to show"
          className="w-full @xl:w-auto"
        >
          {(
            [
              ["short", "Below floor", "Short", short.length],
              ["activity", "With activity", "Activity", activity.length],
              ["all", "All days", "All", rows.length],
            ] as const
          ).map(([value, label, brief, count]) => (
            <ToggleGroupItem
              key={value}
              value={value}
              disabled={value === "short" && !count}
              aria-label={`${label}, ${count} ${count === 1 ? "day" : "days"}`}
              className="h-11 min-w-0 flex-1 gap-1.5 px-2 font-normal data-[state=on]:font-medium @xl:flex-none @xl:px-3"
            >
              <span className="@md:hidden">{brief}</span>
              <span className="hidden @md:inline">{label}</span>
              <span className="text-muted-foreground tabular-nums">
                {count}
              </span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      <div className="mt-3 overflow-hidden rounded-xl border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Date</TableHead>
              <TableHead className="hidden text-right @xl:table-cell">
                In
              </TableHead>
              <TableHead className="hidden text-right @xl:table-cell">
                Out
              </TableHead>
              <TableHead className="text-right">Closing</TableHead>
              <TableHead className="hidden pr-4 text-right @xl:table-cell">
                Shortfall
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {listed.slice(0, limit).map((d) => {
              const expanded = d.date === open;
              const shortfall = BigInt(d.shortfall);
              return [
                <TableRow
                  key={d.date}
                  data-state={expanded ? "selected" : undefined}
                  className="cursor-pointer"
                  onClick={() => setSelected(expanded ? "" : d.date)}
                >
                  <TableCell className="pl-2">
                    <Button
                      variant="ghost"
                      className="h-11 gap-1.5 px-2 font-normal"
                      aria-expanded={expanded}
                      aria-controls={`movements-${d.date}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        setSelected(expanded ? "" : d.date);
                      }}
                    >
                      <ChevronDown
                        className={`size-4 text-muted-foreground transition-transform ${expanded ? "" : "-rotate-90"}`}
                        aria-hidden
                      />
                      <span>{dayLabel(d.date)}</span>
                      <span className="sr-only">
                        {d.movements.length}{" "}
                        {d.movements.length === 1 ? "movement" : "movements"}
                      </span>
                    </Button>
                  </TableCell>
                  <TableCell className="hidden text-right @xl:table-cell">
                    <Amount value={BigInt(d.inflow)} currency={currency} />
                  </TableCell>
                  <TableCell className="hidden text-right @xl:table-cell">
                    <Amount value={BigInt(d.outflow)} currency={currency} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Amount
                      value={BigInt(d.closing)}
                      currency={currency}
                      className="font-medium"
                    />
                    {shortfall > 0n && (
                      <span className="block text-xs text-destructive @xl:hidden">
                        Short {money(shortfall, currency)}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="hidden pr-4 text-right @xl:table-cell">
                    {shortfall > 0n ? (
                      <Amount
                        value={shortfall}
                        currency={currency}
                        tone="shortfall"
                      />
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </TableCell>
                </TableRow>,
                expanded && (
                  <TableRow
                    key={d.date + ":movements"}
                    id={`movements-${d.date}`}
                    className="hover:bg-transparent"
                  >
                    <TableCell
                      colSpan={5}
                      className="whitespace-normal bg-muted/40 p-3 sm:px-6"
                    >
                      <Movements day={d} currency={currency} names={names} />
                    </TableCell>
                  </TableRow>
                ),
              ];
            })}
            {!listed.length && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="whitespace-normal p-8 text-center text-muted-foreground"
                >
                  No days match this filter.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        {listed.length > limit && (
          <div className="border-t p-3 text-center">
            <Button
              variant="ghost"
              className="h-11"
              onClick={() => setLimit(listed.length)}
            >
              Show all {listed.length} days
            </Button>
          </div>
        )}
      </div>
      {forecast.variances.length > 0 && (
        <Collapsible className="mt-4 text-sm">
          <CollapsibleTrigger className="group flex min-h-11 items-center gap-2 text-muted-foreground">
            Actual versus expected ({forecast.variances.length})
            <ChevronDown
              className="size-4 transition-transform group-data-[state=closed]:-rotate-90"
              aria-hidden
            />
          </CollapsibleTrigger>
          <CollapsibleContent className="space-y-2 rounded-lg border bg-card p-4 leading-6">
            {forecast.variances.map((v) => (
              <p key={v.actual.id}>
                {v.expected.name}: expected {dateLabel(v.expected.date)}{" "}
                {money(v.expected.amount, currency)}; actual{" "}
                {dateLabel(v.actual.date)} {money(v.actual.amount, currency)};
                difference {money(v.amount_difference, currency)}.
              </p>
            ))}
          </CollapsibleContent>
        </Collapsible>
      )}
    </section>
  );
});

function Movements({
  day,
  currency,
  names,
}: {
  day: ForecastDay;
  currency: string;
  names: Map<string, string>;
}) {
  if (!day.movements.length)
    return (
      <p className="text-sm text-muted-foreground">
        No movements. Balance carries forward.
      </p>
    );
  return (
    <ul className="divide-y">
      {day.movements.map((m, i) => {
        const e = m.event;
        return (
          <li key={e.id + i} className="py-2.5 text-sm first:pt-0 last:pb-0">
            <div className="flex items-start gap-3">
              <div className="min-w-0 flex-1">
                <p className="font-medium">{e.name}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {statusLabel[e.status] ?? e.status}
                  {e.category ? ` · ${e.category}` : ""}
                </p>
                {e.to_account && (
                  <p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
                    <ArrowRightLeft className="size-3 shrink-0" aria-hidden />
                    {names.get(e.account) ?? e.account} →{" "}
                    {names.get(e.to_account) ?? e.to_account}
                    {" · "}leaves {dateLabel(e.date)}, arrives{" "}
                    {dateLabel(e.arrival ?? e.date)}
                  </p>
                )}
              </div>
              <div className="text-right">
                <Amount
                  value={BigInt(m.delta)}
                  currency={currency}
                  signed
                  className="font-medium"
                />
                <p className="text-xs text-muted-foreground">
                  Balance {money(m.balance, currency)}
                </p>
              </div>
            </div>
            <Collapsible className="text-xs">
              <CollapsibleTrigger className="group -ml-1 flex min-h-11 items-center gap-1 px-1 text-muted-foreground underline-offset-2 hover:underline">
                Evidence
                <ChevronDown
                  className="size-3.5 transition-transform group-data-[state=closed]:-rotate-90"
                  aria-hidden
                />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <p className="leading-5">Evidence: {e.evidence}</p>
              </CollapsibleContent>
            </Collapsible>
          </li>
        );
      })}
    </ul>
  );
}

function BalanceChart({
  rows,
  floor,
  low,
  currency,
  name,
}: {
  rows: ForecastDay[];
  floor: string;
  low?: ForecastDay;
  currency: string;
  name: string;
}) {
  const series = rows.map((d) => ({ date: d.date, amount: d.closing }));
  const limit = BigInt(Number.MAX_SAFE_INTEGER);
  if (series.some((p) => BigInt(p.amount) > limit || BigInt(p.amount) < -limit))
    return (
      <p className="mt-6 text-sm text-muted-foreground">
        These balances are too large to plot accurately. Exact balances are
        listed below.
      </p>
    );
  // Numbers are chart coordinates only; labels and tooltips use exact strings.
  const divisor = 10 ** scale(currency);
  const at = (date: string) => Date.parse(date + "T00:00:00Z");
  const data = series.map((p) => ({
    ...p,
    t: at(p.date),
    y: Number(p.amount) / divisor,
  }));
  const compact = new Intl.NumberFormat("en-US", {
    style: "currency",
    currency,
    notation: "compact",
    maximumFractionDigits: 1,
  });
  return (
    <div
      role="img"
      className="mt-6"
      aria-label={`${name} end-of-day balance from ${dateLabel(series[0].date)} to ${dateLabel(series.at(-1)!.date)}. Lowest ${money(low?.closing ?? series.reduce((a, p) => (BigInt(p.amount) < BigInt(a.amount) ? p : a)).amount, currency)}. Floor ${money(floor, currency)}.`}
    >
      <ChartContainer
        config={{ y: { label: "Closing balance", color: "var(--foreground)" } }}
        className="aspect-auto h-44 w-full sm:h-60"
      >
        <LineChart
          accessibilityLayer={false}
          data={data}
          margin={{ top: 12, right: 12, left: 0, bottom: 0 }}
        >
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis
            dataKey="t"
            type="number"
            domain={["dataMin", "dataMax"]}
            tickFormatter={(v) =>
              dateLabel(new Date(v).toISOString().slice(0, 10))
            }
            minTickGap={40}
            axisLine={false}
            tickLine={false}
            tickMargin={10}
          />
          <YAxis
            width={56}
            axisLine={false}
            tickLine={false}
            tickFormatter={(v) => compact.format(v).replace("-", "−")}
          />
          <Tooltip
            content={({ active, payload }) =>
              active && payload?.[0] ? (
                <div className="rounded-lg border bg-popover p-2.5 text-xs shadow-sm">
                  <p className="text-muted-foreground">
                    {dayLabel(payload[0].payload.date)}
                  </p>
                  <p className="mt-0.5 text-sm font-medium">
                    {money(payload[0].payload.amount, currency)}
                  </p>
                </div>
              ) : null
            }
          />
          <ReferenceLine
            y={Number(floor) / divisor}
            stroke="var(--floor)"
            strokeDasharray="4 4"
          />
          <Line
            type="stepAfter"
            dataKey="y"
            stroke="var(--foreground)"
            strokeWidth={1.75}
            dot={false}
            isAnimationActive={false}
          />
          {low && (
            <ReferenceDot
              x={at(low.date)}
              y={Number(low.closing) / divisor}
              r={4}
              fill="var(--destructive)"
              stroke="var(--card)"
            />
          )}
        </LineChart>
      </ChartContainer>
      <div
        className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-muted-foreground"
        aria-hidden
      >
        <span className="flex items-center gap-2">
          <span className="h-px w-5 bg-foreground" />
          Closing balance
        </span>
        <span className="flex items-center gap-2">
          <span className="w-5 border-t border-dashed border-floor" />
          Floor {money(floor, currency)}
        </span>
        {low && (
          <span className="flex items-center gap-2">
            <span className="size-2 rounded-full bg-destructive" />
            Lowest {dateLabel(low.date)}
          </span>
        )}
      </div>
    </div>
  );
}
