import { useEffect, useRef, type ReactNode } from "react";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ChevronDown,
  Info,
  Landmark,
  TrendingDown,
} from "lucide-react";
import { Amount } from "@/components/amount";
import { DailyBalances, type DayFilter } from "@/components/daily-balances";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { dateLabel, money, rangeLabel, signedMoney, today } from "@/lib/books";
import {
  daysBetween,
  monthName,
  monthSpan,
  scenarioLabel,
  verdict,
  type MonthOutlook,
  type Outlook,
} from "@/lib/forecast";
import type { ForecastState } from "@/lib/use-forecast";

function freshness(o: Outlook) {
  const age = daysBetween(o.asOf, today());
  const when =
    age <= 0 ? "" : age === 1 ? " (yesterday)" : ` (${age} days ago)`;
  return `Estimates from balances on ${dateLabel(o.asOf)}${when} · Not updated automatically`;
}

type Props = {
  scenarios: string[];
  onScenario: (s: string) => void;
  forecast: ForecastState;
  account: string;
  onAccount: (code: string) => void;
  filter?: DayFilter;
  onFilter: (f: DayFilter | undefined) => void;
  focusDaily: boolean;
  onFocused: () => void;
};

export function OutlookPage(props: Props) {
  const { scenarios, onScenario, forecast } = props;
  const { data, outlook, error, loading, scenario } = forecast;
  const daily = useRef<HTMLHeadingElement>(null);
  const shown = data && outlook;
  useEffect(() => {
    if (!props.focusDaily || !shown || loading) return;
    daily.current?.scrollIntoView({ block: "start" });
    daily.current?.focus({ preventScroll: true });
    props.onFocused();
  }, [props.focusDaily, shown, loading, props]);
  const openAccount = (code: string) => {
    props.onAccount(code);
    props.onFilter("short");
    daily.current?.scrollIntoView({ block: "start", behavior: "smooth" });
    daily.current?.focus({ preventScroll: true });
  };
  const currency = data?.plan.currency ?? "USD";
  return (
    <>
      <div className="mb-6 flex flex-col gap-5 sm:mb-8 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <p className="eyebrow">Looking ahead</p>
          <h1 className="page-title">Will income cover spending?</h1>
          <p className="mt-3 text-sm text-muted-foreground sm:text-base">
            {outlook
              ? freshness(outlook)
              : "Estimates from a saved cash plan · Not updated automatically"}
          </p>
        </div>
        {scenarios.length > 1 && (
          <div className="shrink-0 lg:text-right">
            <ToggleGroup
              type="single"
              variant="outline"
              spacing={0}
              value={scenario}
              onValueChange={(v) => v && onScenario(v)}
              aria-label="Scenario"
              className="w-full lg:ml-auto lg:w-fit"
            >
              {scenarios.map((s) => (
                <ToggleGroupItem
                  key={s}
                  value={s}
                  className="h-11 flex-1 bg-card px-5 font-normal data-[state=on]:bg-primary data-[state=on]:font-medium data-[state=on]:text-primary-foreground lg:flex-none"
                >
                  {scenarioLabel(s)}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            <p
              className="mt-2 text-xs text-muted-foreground"
              aria-live="polite"
            >
              {loading && !error
                ? `Loading ${scenarioLabel(scenario)}…`
                : data && !error
                  ? data.plan.name
                  : ""}
            </p>
          </div>
        )}
      </div>
      {error ? (
        <Card className="shadow-none">
          <CardContent className="space-y-4 p-6">
            <h2 className="flex items-center gap-2 font-medium">
              <AlertTriangle className="size-4" />
              {scenarioLabel(scenario)} couldn’t load
            </h2>
            <p role="alert" className="text-sm text-muted-foreground">
              {error}
            </p>
            <div className="flex flex-wrap gap-2">
              <Button className="h-11" onClick={forecast.retry}>
                Try again
              </Button>
              {scenario !== scenarios[0] && (
                <Button
                  variant="outline"
                  className="h-11"
                  onClick={() => onScenario(scenarios[0])}
                >
                  Back to {scenarioLabel(scenarios[0])}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      ) : !shown ? (
        <div className="space-y-6" role="status" aria-label="Loading outlook">
          <Skeleton className="h-80 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
        </div>
      ) : (
        <div
          className={`space-y-6 transition-opacity ${loading ? "opacity-60" : ""}`}
          aria-busy={loading}
        >
          <AnswerCard outlook={outlook} currency={currency} />
          <ReservesCard
            outlook={outlook}
            currency={currency}
            others={scenarios.filter((s) => s !== scenario)}
            onScenario={onScenario}
          />
          <GapsCard
            outlook={outlook}
            currency={currency}
            scenario={scenarioLabel(scenario)}
            others={scenarios.filter((s) => s !== scenario)}
            onScenario={onScenario}
            onView={openAccount}
          />
          <DailyBalances
            ref={daily}
            key={scenario}
            forecast={data}
            outlook={outlook}
            scenario={scenarioLabel(scenario)}
            account={props.account}
            onAccountChange={props.onAccount}
            filter={props.filter}
            onFilterChange={props.onFilter}
          />
          <Method outlook={outlook} forecast={data} />
        </div>
      )}
    </>
  );
}

function AnswerCard({
  outlook,
  currency,
}: {
  outlook: Outlook;
  currency: string;
}) {
  const answer = verdict(outlook);
  const partial = outlook.months.filter((m) => !m.complete);
  const past = today();
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="@container p-5 sm:p-7">
        <h2 className="flex items-start gap-2.5 text-base font-medium sm:text-lg">
          <VerdictIcon tone={answer.tone} />
          {answer.text}
        </h2>
        {outlook.averageLeftOver !== undefined && (
          <div className="mt-4 flex flex-col gap-2 @xl:flex-row @xl:items-end @xl:justify-between">
            <div>
              <p className="text-4xl font-semibold tracking-tight sm:text-5xl">
                {signedMoney(outlook.averageLeftOver, currency)}
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                {outlook.averageLeftOver < 0n ? "short" : "left over"} per month
                on average · {monthSpan(outlook.complete)}
                {outlook.complete.length === 1
                  ? ""
                  : ` · ${outlook.complete.length} full months`}
              </p>
            </div>
            {outlook.tightest && outlook.complete.length > 1 && (
              <p className="text-sm text-muted-foreground">
                Tightest month: {monthName(outlook.tightest.month)},{" "}
                <span className="amount text-foreground">
                  {signedMoney(outlook.tightest.leftOver, currency)}
                </span>
              </p>
            )}
          </div>
        )}
        {outlook.complete.length > 0 && (
          <ul className="mt-6 divide-y border-t" aria-label="Full months">
            {outlook.complete.map((m) => (
              <MonthRow
                key={m.month}
                month={m}
                currency={currency}
                tightest={outlook.complete.length > 1 && m === outlook.tightest}
                elapsed={m.to < past}
              />
            ))}
          </ul>
        )}
        {partial.length > 0 && (
          <ul
            className="mt-2 space-y-2"
            aria-label="Partial months, not included in the average"
          >
            {partial.map((m) => (
              <li key={m.month}>
                <Collapsible className="rounded-lg border">
                  <CollapsibleTrigger className="group flex min-h-11 w-full items-center gap-2 px-3 text-left text-sm text-muted-foreground">
                    <ChevronDown className="size-4 shrink-0 transition-transform group-data-[state=closed]:-rotate-90" />
                    {rangeLabel(m.from, m.to)} · Partial month, not included in
                    the average
                  </CollapsibleTrigger>
                  <CollapsibleContent className="px-3 pb-2">
                    <p className="text-xs leading-5 text-muted-foreground">
                      Only income and spending dated {rangeLabel(m.from, m.to)}{" "}
                      are counted, so this isn’t a monthly rate.
                    </p>
                    <ul>
                      <MonthRow
                        month={m}
                        currency={currency}
                        elapsed={m.to < past}
                      />
                    </ul>
                  </CollapsibleContent>
                </Collapsible>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function VerdictIcon({ tone }: { tone: "covered" | "short" | "none" }) {
  return tone === "covered" ? (
    <CheckCircle2
      className="mt-0.5 size-5 shrink-0 text-positive"
      aria-hidden
    />
  ) : tone === "short" ? (
    <TrendingDown
      className="mt-0.5 size-5 shrink-0 text-destructive"
      aria-hidden
    />
  ) : (
    <Info
      className="mt-0.5 size-5 shrink-0 text-muted-foreground"
      aria-hidden
    />
  );
}

function MonthRow({
  month: m,
  currency,
  tightest,
  elapsed,
}: {
  month: MonthOutlook;
  currency: string;
  tightest?: boolean;
  elapsed?: boolean;
}) {
  const over = m.spending > m.income;
  // Basis points of income spent, for the bar width only.
  const share =
    m.income > 0n
      ? Number((m.spending * 10000n) / m.income) / 100
      : m.spending > 0n
        ? 100
        : 0;
  const label = m.complete ? monthName(m.month) : rangeLabel(m.from, m.to);
  return (
    <li className="py-4">
      <Collapsible>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <CollapsibleTrigger
            className="group -ml-1 flex min-h-11 min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 rounded-md px-1 text-left font-medium"
            aria-label={`${label}: income ${money(m.income, currency)}, spending ${money(m.spending, currency)}, ${m.leftOver < 0n ? "short" : "left over"} ${money(m.leftOver < 0n ? -m.leftOver : m.leftOver, currency)}. Spending by category`}
          >
            {label}
            <ChevronDown className="size-4 text-muted-foreground transition-transform group-data-[state=closed]:-rotate-90" />
            {tightest && <Badge variant="outline">Tightest</Badge>}
            {elapsed && <Badge variant="secondary">Past · estimate</Badge>}
          </CollapsibleTrigger>
          <p className="ml-auto flex flex-wrap items-baseline justify-end gap-x-6 gap-y-1 text-right text-sm">
            <span className="hidden @2xl:inline">
              <span className="stat-label mr-2">Income</span>
              <Amount value={m.income} currency={currency} />
            </span>
            <span className="hidden @2xl:inline">
              <span className="stat-label mr-2">Spending</span>
              <Amount value={m.spending} currency={currency} />
            </span>
            <span>
              <span className="stat-label mr-2">
                {m.leftOver < 0n ? "Short" : "Left over"}
              </span>
              <Amount
                value={m.leftOver}
                currency={currency}
                signed
                tone="balance"
                className="font-semibold"
              />
            </span>
          </p>
        </div>
        <p className="text-xs text-muted-foreground @2xl:hidden">
          Income{" "}
          <Amount
            value={m.income}
            currency={currency}
            className="text-foreground"
          />
          <span className="mx-1.5">·</span>
          Spending{" "}
          <Amount
            value={m.spending}
            currency={currency}
            className="text-foreground"
          />
        </p>
        <div
          className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-muted"
          aria-hidden
        >
          <div
            className={`h-full rounded-full ${over ? "bg-destructive" : "bg-foreground/80"}`}
            style={{ width: `${Math.min(share, 100)}%` }}
          />
        </div>
        <CollapsibleContent>
          <p className="mt-4 stat-label">Spending by category</p>
          <ul className="mt-2 grid gap-x-8 gap-y-1.5 text-sm @xl:grid-cols-2">
            {m.categories.map((c) => (
              <li key={c.name} className="flex justify-between gap-4">
                <span className="min-w-0 text-muted-foreground">{c.name}</span>
                <Amount value={c.amount} currency={currency} />
              </li>
            ))}
            {!m.categories.length && (
              <li className="text-muted-foreground">
                No spending in this period.
              </li>
            )}
          </ul>
        </CollapsibleContent>
      </Collapsible>
    </li>
  );
}

function ReservesCard({
  outlook,
  currency,
  others,
  onScenario,
}: {
  outlook: Outlook;
  currency: string;
  others: string[];
  onScenario: (s: string) => void;
}) {
  const months = outlook.complete.length ? outlook.complete : outlook.months;
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="@container p-5 sm:p-7">
        <h2 className="section-title text-base">After reserve contributions</h2>
        {!outlook.reserveActivity ? (
          <div className="mt-2 flex flex-col gap-3 @xl:flex-row @xl:items-center @xl:justify-between">
            <p className="text-sm text-muted-foreground">
              This scenario doesn’t move money into or out of reserve accounts.
            </p>
            {others.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {others.map((s) => (
                  <Button
                    key={s}
                    variant="outline"
                    className="h-11"
                    onClick={() => onScenario(s)}
                  >
                    Compare {scenarioLabel(s)}
                    <ArrowRight />
                  </Button>
                ))}
              </div>
            )}
          </div>
        ) : (
          <>
            <p className="mt-2 text-sm text-muted-foreground">
              {outlook.averageAfterReserves !== undefined &&
              outlook.averageAfterReserves < 0n
                ? `Reserve transfers in this scenario take more than is left over, so they draw on existing balances: ${signedMoney(outlook.averageAfterReserves, currency)} a month on average.`
                : outlook.averageAfterReserves !== undefined
                  ? `Reserve transfers in this scenario fit within what’s left over: ${signedMoney(outlook.averageAfterReserves, currency)} a month on average after reserves.`
                  : "Reserve transfers in this scenario, by partial month."}
            </p>
            <div className="mt-5 grid gap-4 @3xl:grid-cols-3">
              {months.map((m) => (
                <dl key={m.month} className="rounded-lg border p-4 text-sm">
                  <dt className="mb-2 font-medium">
                    {m.complete ? monthName(m.month) : rangeLabel(m.from, m.to)}
                  </dt>
                  <Line label={m.leftOver < 0n ? "Short" : "Left over"}>
                    <Amount value={m.leftOver} currency={currency} signed />
                  </Line>
                  <Line
                    label={m.toReserves < 0n ? "From reserves" : "To reserves"}
                  >
                    <Amount value={-m.toReserves} currency={currency} signed />
                  </Line>
                  {m.reserves.map((r) => (
                    <Line key={r.code} label={r.name} sub>
                      <Amount value={r.amount} currency={currency} />
                    </Line>
                  ))}
                  <div className="mt-2 border-t pt-2">
                    <Line label="After reserves" strong>
                      <Amount
                        value={m.afterReserves}
                        currency={currency}
                        signed
                        tone="balance"
                      />
                    </Line>
                  </div>
                </dl>
              ))}
            </div>
            <p className="mt-4 flex items-start gap-2 text-xs leading-5 text-muted-foreground">
              <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden />
              These are the transfers in this scenario. They don’t show whether
              full reserve targets are met.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function Line({
  label,
  children,
  sub,
  strong,
}: {
  label: string;
  children: ReactNode;
  sub?: boolean;
  strong?: boolean;
}) {
  return (
    <div
      className={`flex items-baseline justify-between gap-3 py-0.5 ${sub ? "pl-3 text-xs text-muted-foreground" : ""} ${strong ? "font-semibold" : ""}`}
    >
      <span
        className={`min-w-0 ${sub || strong ? "" : "text-muted-foreground"}`}
      >
        {label}
      </span>
      {children}
    </div>
  );
}

function GapsCard({
  outlook,
  currency,
  scenario,
  others,
  onScenario,
  onView,
}: {
  outlook: Outlook;
  currency: string;
  scenario: string;
  others: string[];
  onScenario: (s: string) => void;
  onView: (code: string) => void;
}) {
  // A scenario without proposed transfers usually leaves funded accounts empty.
  const compare =
    outlook.gaps.length > 0 && !outlook.counts.proposed && others.length > 0;
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="@container p-5 sm:p-7">
        <h2 className="section-title text-base">Accounts that run short</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Balanced overall doesn’t mean every account has money on the right
          day.
        </p>
        {compare && (
          <div className="mt-4 flex flex-col gap-3 rounded-lg bg-muted/60 p-4 text-sm @xl:flex-row @xl:items-center @xl:justify-between">
            <p className="text-muted-foreground">
              {scenario} includes no proposed transfers, so accounts that rely
              on them run short here.
            </p>
            <div className="flex flex-wrap gap-2">
              {others.map((s) => (
                <Button
                  key={s}
                  variant="outline"
                  className="h-11 bg-card"
                  onClick={() => onScenario(s)}
                >
                  Compare {scenarioLabel(s)}
                  <ArrowRight />
                </Button>
              ))}
            </div>
          </div>
        )}
        {outlook.gaps.length === 0 ? (
          <p className="mt-4 flex items-center gap-2 text-sm">
            <CheckCircle2 className="size-4 text-positive" aria-hidden />
            No bank account drops below its floor in {scenario}.
          </p>
        ) : (
          <ul className="mt-4 divide-y border-t">
            {outlook.gaps.map((g) => (
              <li
                key={g.code}
                className="flex flex-col gap-3 py-4 @xl:flex-row @xl:items-center"
              >
                <div className="flex min-w-0 flex-1 items-start gap-3">
                  <span className="icon-tile">
                    <Landmark />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="flex flex-wrap items-center gap-2 text-sm font-medium">
                      {g.name}
                      {g.reserved && <Badge variant="outline">Reserve</Badge>}
                    </p>
                    <p className="mt-1 text-xs leading-5 text-muted-foreground">
                      Below its <Amount value={g.floor} currency={currency} />{" "}
                      floor from {dateLabel(g.firstDate)} · {g.daysShort}{" "}
                      {g.daysShort === 1 ? "day" : "days"} · lowest{" "}
                      <Amount value={g.lowest} currency={currency} /> on{" "}
                      {dateLabel(g.lowestDate)}
                    </p>
                  </div>
                  <p className="text-right text-sm @xl:hidden">
                    <span className="block text-xs text-muted-foreground">
                      Short
                    </span>
                    <Amount
                      value={g.shortfall}
                      currency={currency}
                      tone="shortfall"
                      className="font-semibold"
                    />
                  </p>
                </div>
                <p className="hidden text-right text-sm @xl:block">
                  <span className="mr-2 text-xs text-muted-foreground">
                    Short
                  </span>
                  <Amount
                    value={g.shortfall}
                    currency={currency}
                    tone="shortfall"
                    className="font-semibold"
                  />
                </p>
                <Button
                  variant="outline"
                  className="h-11 w-full @xl:w-auto"
                  onClick={() => onView(g.code)}
                  aria-label={`View daily balances for ${g.name}`}
                >
                  View daily balances
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function Method({
  outlook,
  forecast,
}: {
  outlook: Outlook;
  forecast: NonNullable<ForecastState["data"]>;
}) {
  const c = outlook.counts;
  const statuses = (["actual", "confirmed", "estimated", "proposed"] as const)
    .filter((s) => c[s])
    .map((s) => `${c[s]} ${s}`)
    .join(", ");
  return (
    <Collapsible className="mt-2">
      <CollapsibleTrigger className="group flex min-h-11 items-center gap-2 text-sm text-muted-foreground">
        How this is calculated
        <ChevronDown className="size-4 transition-transform group-data-[state=closed]:-rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-4 rounded-xl border bg-card p-5 text-sm leading-6 text-muted-foreground">
        <ul className="list-disc space-y-1 pl-5">
          <li>
            <strong className="font-medium text-foreground">Income</strong> is
            every expected inflow in the plan.{" "}
            <strong className="font-medium text-foreground">Spending</strong> is
            every outflow from a bank account plus purchases on cards. Card
            payments and transfers between accounts aren’t counted again.
          </li>
          <li>
            <strong className="font-medium text-foreground">
              Net to reserves
            </strong>{" "}
            is money moved into reserve accounts, less money moved or spent out
            of them.{" "}
            <strong className="font-medium text-foreground">
              After reserves
            </strong>{" "}
            is how non-reserve money changes.
          </li>
          <li>
            Months follow the date money leaves or arrives. Only full calendar
            months count toward averages. Balances are end of day; intraday
            availability isn’t established.
          </li>
          <li>
            This plan covers{" "}
            {rangeLabel(
              outlook.months[0]?.from ?? outlook.through,
              outlook.through,
            )}{" "}
            with {outlook.accounts.bank} bank accounts (
            {outlook.accounts.reserved} reserve) and {outlook.accounts.card}{" "}
            {outlook.accounts.card === 1 ? "card" : "cards"}. Accounts left out
            of the plan, such as excluded owners or investments, aren’t
            operating cash here. Its {c.total} future events are{" "}
            {statuses || "none"}.
          </li>
          <li>
            Opening balances are snapshots from {dateLabel(outlook.asOf)}. Books
            recalculates this saved plan when you open it; it doesn’t fetch bank
            balances, post entries or move money.
          </li>
        </ul>
        {(forecast.plan.assumptions?.length ?? 0) + forecast.warnings.length >
          0 && (
          <div>
            <h3 className="font-medium text-foreground">Plan assumptions</h3>
            <ul className="mt-1 list-disc space-y-1 pl-5">
              {[...(forecast.plan.assumptions ?? []), ...forecast.warnings]
                .filter(
                  (s) => !s.startsWith("Includes proposed transfer/event: "),
                )
                .map((s, i) => (
                  <li key={i}>{s}</li>
                ))}
            </ul>
          </div>
        )}
        <p className="break-all text-xs">Plan digest: {forecast.digest}</p>
      </CollapsibleContent>
    </Collapsible>
  );
}

export function OutlookSummary({
  forecast,
  onOpen,
}: {
  forecast: ForecastState;
  onOpen: () => void;
}) {
  const { outlook, data, error, loading } = forecast;
  const currency = data?.plan.currency ?? "USD";
  return (
    <Card className="mb-7 gap-0 py-0 shadow-none">
      <CardContent className="@container p-5 sm:p-7">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-medium">Looking ahead</h2>
          <Badge variant="outline">Estimates</Badge>
        </div>
        {error ? (
          <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
            <p role="alert" className="text-sm text-muted-foreground">
              The outlook couldn’t load. {error}
            </p>
            <Button variant="outline" className="h-11" onClick={forecast.retry}>
              Try again
            </Button>
          </div>
        ) : !outlook || (loading && !outlook) ? (
          <div
            className="mt-3 space-y-2"
            role="status"
            aria-label="Loading outlook"
          >
            <Skeleton className="h-6 w-2/3" />
            <Skeleton className="h-10 w-40" />
          </div>
        ) : (
          (() => {
            const answer = verdict(outlook);
            const gaps = outlook.gaps;
            return (
              <div className="mt-3 flex flex-col gap-5 @xl:flex-row @xl:items-end @xl:justify-between">
                <div className="min-w-0">
                  <p className="flex items-start gap-2 text-base font-medium">
                    <VerdictIcon tone={answer.tone} />
                    {answer.text}
                  </p>
                  {outlook.averageLeftOver !== undefined && (
                    <p className="mt-2 text-sm text-muted-foreground">
                      <span className="amount mr-1.5 text-2xl font-semibold tracking-tight text-foreground">
                        {signedMoney(outlook.averageLeftOver, currency)}
                      </span>
                      {outlook.averageLeftOver < 0n ? "short" : "left over"} a
                      month · {monthSpan(outlook.complete)}
                    </p>
                  )}
                  <p className="mt-2 text-xs text-muted-foreground">
                    {scenarioLabel(forecast.scenario)}:{" "}
                    {gaps.length === 0
                      ? "no bank account drops below its floor"
                      : gaps.length === 1
                        ? `${gaps[0].name} runs short from ${dateLabel(gaps[0].firstDate)}`
                        : `${gaps.length} accounts run short`}{" "}
                    · Balances from {dateLabel(outlook.asOf)}
                  </p>
                </div>
                <Button className="h-11 shrink-0" onClick={onOpen}>
                  Open outlook
                  <ArrowRight />
                </Button>
              </div>
            );
          })()
        )}
      </CardContent>
    </Card>
  );
}
