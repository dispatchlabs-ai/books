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
  average,
  daysBetween,
  monthName,
  monthSpan,
  scenarioLabel,
  spentShare,
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
const monthLabel = (m: MonthOutlook) =>
  m.complete ? monthName(m.month) : rangeLabel(m.from, m.to);
const exclusion = {
  partial: {
    title: "Partial month, not included in the average",
    detail: (m: MonthOutlook) =>
      `Only income and spending dated ${rangeLabel(m.from, m.to)} are counted, so this isn’t a monthly rate.`,
  },
  ended: {
    title: "Already ended, not included",
    detail: () =>
      "These are the plan’s earlier estimates, not what actually happened.",
  },
  unplanned: {
    title: "Nothing planned, not included",
    detail: () => "The plan has no income or spending in this month.",
  },
};
function scrollBehavior(): ScrollBehavior {
  return matchMedia("(prefers-reduced-motion: reduce)").matches
    ? "auto"
    : "smooth";
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
  const { scenarios, onScenario, forecast, focusDaily, onFocused } = props;
  const { data, outlook, error, loading, scenario, shown } = forecast;
  const daily = useRef<HTMLHeadingElement>(null);
  const ready = Boolean(data && outlook) && !loading;
  useEffect(() => {
    if (!focusDaily || !ready) return;
    daily.current?.scrollIntoView({ block: "start" });
    daily.current?.focus({ preventScroll: true });
    onFocused();
  }, [focusDaily, ready, onFocused]);
  const openAccount = (code: string) => {
    props.onAccount(code);
    props.onFilter("short");
    // The section stays mounted across accounts, so it can take focus now.
    daily.current?.scrollIntoView({
      block: "start",
      behavior: scrollBehavior(),
    });
    daily.current?.focus({ preventScroll: true });
  };
  const currency = data?.plan.currency ?? "USD";
  // Labels follow the scenario whose figures are on screen, not the one loading.
  const label = scenarioLabel(shown);
  const others = loading ? [] : scenarios.filter((s) => s !== shown);
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
              {error
                ? ""
                : loading
                  ? `Loading ${scenarioLabel(scenario)}…`
                  : (data?.plan.name ?? "")}
            </p>
          </div>
        )}
      </div>
      {error ? (
        <Card className="shadow-none">
          <CardContent className="space-y-4 p-6">
            <h2 className="flex items-center gap-2 font-medium">
              <AlertTriangle className="size-4" aria-hidden />
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
      ) : !data || !outlook ? (
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
          <ReservesCard outlook={outlook} currency={currency} />
          <GapsCard
            outlook={outlook}
            currency={currency}
            scenario={label}
            others={others}
            onScenario={onScenario}
            onView={openAccount}
          />
          <DailyBalances
            ref={daily}
            key={shown}
            forecast={data}
            outlook={outlook}
            scenario={label}
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
  const excluded = outlook.months.filter((m) => m.excluded);
  const ended = excluded.filter((m) => m.excluded === "ended").length;
  const counted = outlook.counted;
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="@container p-5 sm:p-7">
        <h2 className="flex items-start gap-2.5 text-base font-medium sm:text-lg">
          <VerdictIcon tone={answer.tone} />
          {answer.text}
        </h2>
        {ended > 0 && counted.length > 0 && (
          <p className="mt-3 flex items-start gap-2 rounded-lg bg-muted/60 p-3 text-sm text-muted-foreground">
            <Info className="mt-0.5 size-4 shrink-0" aria-hidden />
            {ended === 1 ? "One month" : `${ended} months`} in this plan{" "}
            {ended === 1 ? "has" : "have"} already ended and{" "}
            {ended === 1 ? "isn’t" : "aren’t"} counted. Update the plan with
            actual activity to keep this answer current.
          </p>
        )}
        {outlook.averageLeftOver !== undefined && (
          <div className="mt-4 flex flex-col gap-2 @xl:flex-row @xl:items-end @xl:justify-between">
            <div>
              <p className="text-4xl font-semibold tracking-tight sm:text-5xl">
                {signedMoney(outlook.averageLeftOver, currency)}
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                {outlook.averageLeftOver < 0n ? "short" : "left over"} per month
                on average · {monthSpan(counted)}
                {counted.length === 1 ? "" : ` · ${counted.length} full months`}
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                Monthly averages: income{" "}
                <Amount
                  value={average(counted.map((m) => m.income))!}
                  currency={currency}
                />
                {" · "}spending{" "}
                <Amount
                  value={average(counted.map((m) => m.spending))!}
                  currency={currency}
                />
                . Before reserve goals.
              </p>
            </div>
            {outlook.tightest && counted.length > 1 && (
              <p className="text-sm text-muted-foreground">
                Tightest month: {monthName(outlook.tightest.month)},{" "}
                <span className="amount text-foreground">
                  {signedMoney(outlook.tightest.leftOver, currency)}
                </span>
              </p>
            )}
          </div>
        )}
        {counted.length > 0 && (
          <ul className="mt-6 divide-y border-t" aria-label="Full months">
            {counted.map((m) => (
              <li key={m.month} className="py-4">
                <MonthRow
                  month={m}
                  currency={currency}
                  tightest={counted.length > 1 && m === outlook.tightest}
                />
              </li>
            ))}
          </ul>
        )}
        {excluded.length > 0 && (
          <ul
            className="mt-2 space-y-2"
            aria-label="Months not included in the answer"
          >
            {excluded.map((m) => (
              <li key={m.month}>
                <Excluded month={m}>
                  <MonthRow month={m} currency={currency} plain />
                </Excluded>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function Excluded({
  month: m,
  children,
}: {
  month: MonthOutlook;
  children: ReactNode;
}) {
  const reason = exclusion[m.excluded!];
  return (
    <Collapsible className="rounded-lg border">
      <CollapsibleTrigger className="group flex min-h-11 w-full items-center gap-2 px-3 py-2 text-left text-sm text-muted-foreground">
        <ChevronDown
          className="size-4 shrink-0 transition-transform group-data-[state=closed]:-rotate-90"
          aria-hidden
        />
        {monthLabel(m)} · {reason.title}
      </CollapsibleTrigger>
      <CollapsibleContent className="px-3 pb-3">
        <p className="mb-2 text-xs leading-5 text-muted-foreground">
          {reason.detail(m)}
        </p>
        {children}
      </CollapsibleContent>
    </Collapsible>
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

// One month's income, spending and what's left. `plain` shows figures for a
// month outside the answer: no status color and no nested disclosure.
function MonthRow({
  month: m,
  currency,
  tightest,
  plain,
}: {
  month: MonthOutlook;
  currency: string;
  tightest?: boolean;
  plain?: boolean;
}) {
  const over = m.spending > m.income;
  const share = spentShare(m);
  // Tenths of a percent of income spent, for the bar width only.
  const width =
    m.income > 0n
      ? Math.max(0, Math.min(Number((m.spending * 1000n) / m.income) / 10, 100))
      : m.spending > 0n
        ? 100
        : 0;
  const label = monthLabel(m);
  const leftLabel = m.leftOver < 0n ? "Short" : "Left over";
  const summary = (
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
        <span className="stat-label mr-2">{leftLabel}</span>
        <Amount
          value={m.leftOver}
          currency={currency}
          signed
          tone={plain ? "none" : "balance"}
          className="font-semibold"
        />
      </span>
    </p>
  );
  const details = (
    <>
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
      <div className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full ${over && !plain ? "bg-destructive" : "bg-foreground/80"}`}
          style={{ width: `${width}%` }}
        />
      </div>
      <p className="mt-1 text-right text-xs text-muted-foreground">
        {m.spending < 0n
          ? "Card credits exceed planned spending"
          : share
            ? `${share} of income spent`
            : "No income planned"}
      </p>
    </>
  );
  if (plain)
    return (
      <div>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <p className="font-medium">{label}</p>
          {summary}
        </div>
        {details}
      </div>
    );
  return (
    <Collapsible>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <CollapsibleTrigger
          className="group -ml-1 flex min-h-11 min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 rounded-md px-1 text-left font-medium"
          aria-label={`${label}${tightest ? ", tightest month" : ""}: income ${money(m.income, currency)}, spending ${money(m.spending, currency)}, ${leftLabel.toLowerCase()} ${money(m.leftOver < 0n ? -m.leftOver : m.leftOver, currency)}. Spending by category`}
        >
          {label}
          <ChevronDown
            className="size-4 text-muted-foreground transition-transform group-data-[state=closed]:-rotate-90"
            aria-hidden
          />
          {tightest && <Badge variant="outline">Tightest</Badge>}
        </CollapsibleTrigger>
        {summary}
      </div>
      {details}
      <CollapsibleContent>
        <p className="stat-label mt-3">Spending by category</p>
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
  );
}

function ReservesCard({
  outlook,
  currency,
}: {
  outlook: Outlook;
  currency: string;
}) {
  const after = outlook.averageAfterReserves;
  const extra = outlook.months.filter(
    (m) => m.excluded && m.reserves.length > 0,
  );
  return (
    <Card className="gap-0 py-0 shadow-none">
      <CardContent className="@container p-5 sm:p-7">
        <h2 className="section-title">Reserve balances and goals</h2>
        <p className="mt-3 rounded-lg border bg-muted/40 p-4 text-sm leading-6">
          <strong className="font-medium">
            Full reserve goals aren’t assessed.
          </strong>{" "}
          This forecast contains planned movements, but no structured reserve
          targets, contribution schedules or cap rules. It cannot establish
          whether income covers your full reserve goals. Planned funding may
          cover only part of them.
        </p>
        {!outlook.reserveActivity ? (
          <p className="mt-2 text-sm text-muted-foreground">
            This scenario has no reserve account activity.
          </p>
        ) : (
          <>
            <p className="mt-2 text-sm text-muted-foreground">
              {!outlook.counted.length
                ? "Reserve activity in this scenario falls only in months outside the answer."
                : after! < 0n
                  ? `After planned spending, reserve balance changes and transfer timing, other bank and card balances decrease by ${money(-after!, currency)} a month on average.`
                  : `After planned spending, reserve balance changes and transfer timing, other bank and card balances change by ${signedMoney(after!, currency)} a month on average.`}
            </p>
            {outlook.counted.length > 0 && (
              <div className="mt-5 grid gap-4 @3xl:grid-cols-3">
                {outlook.counted.map((m) => (
                  <ReserveMonth
                    key={m.month}
                    month={m}
                    currency={currency}
                    boxed
                  />
                ))}
              </div>
            )}
            {extra.length > 0 && (
              <ul className="mt-3 space-y-2">
                {extra.map((m) => (
                  <li key={m.month}>
                    <Excluded month={m}>
                      <ReserveMonth month={m} currency={currency} />
                    </Excluded>
                  </li>
                ))}
              </ul>
            )}
            <p className="mt-4 flex items-start gap-2 text-xs leading-5 text-muted-foreground">
              <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden />
              Reserve changes include deposits, withdrawals and reserve-paid
              expenses. They are net balance changes, not contribution totals.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function ReserveMonth({
  month: m,
  currency,
  boxed,
}: {
  month: MonthOutlook;
  currency: string;
  boxed?: boolean;
}) {
  return (
    <section
      aria-label={`${monthLabel(m)} reserves`}
      className={`text-sm ${boxed ? "rounded-lg border p-4" : ""}`}
    >
      {boxed && <h3 className="mb-2 font-medium">{monthLabel(m)}</h3>}
      <dl>
        <Line label={m.leftOver < 0n ? "Short" : "Left over"}>
          <Amount value={m.leftOver} currency={currency} signed />
        </Line>
        <Line label="Less reserve balance change">
          <Amount value={-m.toReserves} currency={currency} signed />
        </Line>
        {m.reserves.map((r) => (
          <Line key={r.code} label={r.name} sub>
            <Amount value={-r.amount} currency={currency} signed />
          </Line>
        ))}
        {m.inTransit !== 0n && (
          <Line label="Less change in transit">
            <Amount value={-m.inTransit} currency={currency} signed />
          </Line>
        )}
        <div className="mt-2 border-t pt-2">
          <Line label="Change in other balances" strong>
            <Amount
              value={m.afterReserves}
              currency={currency}
              signed
              tone={boxed ? "balance" : "none"}
            />
          </Line>
        </div>
      </dl>
    </section>
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
      <dt className={`min-w-0 ${sub || strong ? "" : "text-muted-foreground"}`}>
        {label}
      </dt>
      <dd>{children}</dd>
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
        <h2 className="section-title">Accounts that run short</h2>
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
  const strong = (text: string) => (
    <strong className="font-medium text-foreground">{text}</strong>
  );
  return (
    <Collapsible className="mt-2">
      <CollapsibleTrigger className="group flex min-h-11 items-center gap-2 text-sm text-muted-foreground">
        How this is calculated
        <ChevronDown
          className="size-4 transition-transform group-data-[state=closed]:-rotate-90"
          aria-hidden
        />
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-4 rounded-xl border bg-card p-5 text-sm leading-6 text-muted-foreground">
        <ul className="list-disc space-y-1 pl-5">
          <li>
            {strong("Income")} is every expected inflow to a bank account.{" "}
            {strong("Spending")} is every outflow from a bank account plus card
            purchases, less refunds and credits on cards. Card payments and
            transfers between accounts in the plan aren’t spending. Money sent
            to an account outside the plan, such as a brokerage contribution, is
            an outflow, so it counts as spending here.
          </li>
          <li>
            {strong("Reserve balance change")} includes deposits, withdrawals
            and reserve-paid expenses. Subtract it and the change in money in
            transit from what’s left over to get{" "}
            {strong("Change in other balances")}: the net change in non-reserve
            bank and card balances. A negative reserve change releases existing
            reserves; it is not new income.
          </li>
          <li>
            Transfers leave on departure and reach the destination on arrival.
            Between those dates the money is in transit. Only full months with
            planned activity that haven’t ended count toward the answer and
            averages. Balances are end of day; intraday availability isn’t
            established.
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
            evaluates the saved plan when this page first loads and when you
            refresh; it doesn’t fetch bank balances, post entries or move money.
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
  const { outlook, data, error } = forecast;
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
        ) : !outlook ? (
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
                <div className="min-w-0 space-y-2">
                  <p className="flex items-start gap-2 text-base font-medium">
                    <VerdictIcon tone={answer.tone} />
                    {answer.text}
                  </p>
                  {outlook.averageLeftOver !== undefined && (
                    <p className="text-sm text-muted-foreground">
                      <span className="amount mr-1.5 text-2xl font-semibold tracking-tight text-foreground">
                        {signedMoney(outlook.averageLeftOver, currency)}
                      </span>
                      {outlook.averageLeftOver < 0n ? "short" : "left over"} a
                      month · {monthSpan(outlook.counted)}
                    </p>
                  )}
                  {outlook.reserveActivity &&
                    outlook.averageAfterReserves !== undefined && (
                      <p className="text-sm text-muted-foreground">
                        Change in other balances:{" "}
                        <Amount
                          value={outlook.averageAfterReserves}
                          currency={currency}
                          signed
                          tone="balance"
                          className="font-medium"
                        />{" "}
                        a month
                      </p>
                    )}
                  <p className="flex items-start gap-2 text-sm">
                    {gaps.length ? (
                      <AlertTriangle
                        className="mt-0.5 size-4 shrink-0 text-destructive"
                        aria-hidden
                      />
                    ) : (
                      <CheckCircle2
                        className="mt-0.5 size-4 shrink-0 text-positive"
                        aria-hidden
                      />
                    )}
                    <span>
                      {scenarioLabel(forecast.shown)}:{" "}
                      {gaps.length === 0
                        ? "no bank account drops below its floor"
                        : gaps.length === 1
                          ? `${gaps[0].name} runs short from ${dateLabel(gaps[0].firstDate)}`
                          : `${gaps.length} accounts run short`}
                      <span className="text-muted-foreground">
                        {" "}
                        · Balances from {dateLabel(outlook.asOf)}
                      </span>
                    </span>
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
