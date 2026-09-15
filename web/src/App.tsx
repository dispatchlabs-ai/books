import { useEffect, useRef, useState } from "react";
import {
  ArrowDownLeft,
  ArrowUpRight,
  ArrowUp,
  ArrowRight,
  ChevronDown,
  ChevronLeft,
  CreditCard,
  Home,
  Landmark,
  MessageCircle,
  Search,
  Sparkles,
  Wrench,
  RefreshCw,
  Check,
  Building2,
  CalendarRange,
  Clock3,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Skeleton } from "@/components/ui/skeleton";
import { OutlookPage, OutlookSummary } from "@/components/outlook";
import type { DayFilter } from "@/components/daily-balances";
import { CashChart } from "@/components/cash-chart";
import { Decision } from "@/components/decision";
import {
  today,
  conversationContext,
  dateLabel,
  loadSnapshot,
  money,
  request,
  sum,
  type Account,
  type Answer,
  type Company,
  type Snapshot,
} from "@/lib/books";
import { demoCompanies, demoSnapshot } from "@/lib/demo";
import { useForecast } from "@/lib/use-forecast";

type Page = "overview" | "outlook" | "ask" | "account";
type Config = {
  demo: boolean;
  agent: boolean;
  forecasts?: Record<string, string[]>;
};
const pageNames = {
  overview: "Overview",
  outlook: "Outlook",
  ask: "Ask Books",
};
const pageIcons = {
  overview: Home,
  outlook: CalendarRange,
  ask: MessageCircle,
};
type Message = {
  role: "user" | "assistant";
  content: string;
  sources?: Answer["sources"];
};
function App() {
  const [config, setConfig] = useState<Config>();
  const [companies, setCompanies] = useState<Company[]>([]);
  const [company, setCompany] = useState<Company>();
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    request<Config>("/config", controller.signal)
      .then(async (cfg) => {
        const list = cfg.demo
          ? demoCompanies
          : await request<Company[]>("/books/companies", controller.signal);
        if (controller.signal.aborted) return;
        setConfig(cfg);
        setCompanies(list);
        setCompany(list[0]);
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e.message);
      });
    return () => controller.abort();
  }, [retry]);
  if (error)
    return (
      <div className="grid min-h-screen place-items-center p-6">
        <div className="max-w-sm space-y-4">
          <h1 className="text-2xl font-semibold">Let’s reconnect Books</h1>
          <p role="alert" className="text-muted-foreground">
            {error}
          </p>
          <Button
            onClick={() => {
              setError("");
              setRetry((v) => v + 1);
            }}
          >
            Try again
          </Button>
        </div>
      </div>
    );
  if (!config) return <Loading />;
  if (!company)
    return (
      <div className="grid min-h-screen place-items-center p-6">
        <div>
          <h1 className="text-2xl font-semibold">No entities available</h1>
          <p className="mt-3 text-muted-foreground">
            Your Books connection has no accessible entities.
          </p>
          <Button
            className="mt-4"
            onClick={() => {
              setError("");
              setRetry((v) => v + 1);
            }}
          >
            Refresh
          </Button>
        </div>
      </div>
    );
  return (
    <Workspace
      key={`${config.demo}:${company.key}`}
      config={config}
      companies={companies}
      company={company}
      selectCompany={setCompany}
    />
  );
}
function Loading() {
  return (
    <div
      className="mx-auto max-w-4xl space-y-6 p-8"
      role="status"
      aria-label="Loading Books"
    >
      <Skeleton className="h-9 w-40" />
      <Skeleton className="h-16 w-3/4" />
      <Skeleton className="h-80 w-full" />
      <span className="sr-only">Loading Books</span>
    </div>
  );
}
function Workspace({
  config,
  companies,
  company,
  selectCompany,
}: {
  config: Config;
  companies: Company[];
  company: Company;
  selectCompany: (c: Company) => void;
}) {
  const [snapshot, setSnapshot] = useState<Snapshot | undefined>(() =>
    config.demo ? demoSnapshot(company) : undefined,
  );
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [periodError, setPeriodError] = useState("");
  const [range, setRange] = useState({
    from: today().slice(0, 7) + "-01",
    to: today(),
  });
  const [page, setPage] = useState<Page>("overview");
  const [accountID, setAccountID] = useState<string>();
  const [decision, setDecision] = useState(false);
  const scenarios = config.demo ? [] : (config.forecasts?.[company.key] ?? []);
  const [scenario, setScenario] = useState(scenarios[0] ?? "");
  const forecast = useForecast(company.key, scenario);
  const [outlookAccount, setOutlookAccount] = useState("");
  const [dayFilter, setDayFilter] = useState<DayFilter>();
  const [focusDaily, setFocusDaily] = useState(false);
  const pages = (["overview", "outlook", "ask"] as const).filter(
    (p) => p !== "outlook" || scenarios.length > 0,
  );
  const [plan, setPlan] = useState(() => {
    try {
      const value = localStorage.getItem(`books.demo.plan.${company.key}`);
      return value === "now" || value === "later" ? value : "";
    } catch {
      return "";
    }
  });
  const [decisionChoice, setDecisionChoice] = useState(plan || "later");
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [asking, setAsking] = useState(false);
  const [askError, setAskError] = useState("");
  const askController = useRef<AbortController | null>(null);
  const end = useRef<HTMLDivElement>(null);
  const composer = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const controller = new AbortController();
    if (!config.demo)
      loadSnapshot(company, controller.signal, range)
        .then((s) => {
          if (!controller.signal.aborted) setSnapshot(s);
        })
        .catch((e) => {
          if (!controller.signal.aborted) setError(e.message);
        });
    return () => controller.abort();
  }, [company, config.demo, revision, range]);
  useEffect(() => () => askController.current?.abort(), []);
  useEffect(() => {
    if (page === "ask" && messages.length)
      end.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages, page]);
  const account = snapshot?.accounts.find((a) => a.id === accountID);
  const current = page === "account" ? "overview" : page;
  const business = company.key === "studio";
  const resetConversation = () => {
    askController.current?.abort();
    setAsking(false);
    setMessages([]);
    setDraft("");
    setAskError("");
  };
  const navigate = (next: Page) => {
    setPage(next);
    if (next === "overview" && accountID) {
      resetConversation();
      setAccountID(undefined);
    }
    window.scrollTo({ top: 0 });
  };
  const openAccount = (id: string) => {
    if (id !== accountID) resetConversation();
    setAccountID(id);
    navigate("account");
  };
  const openOutlook = (accountCode?: string) => {
    if (accountCode) {
      setOutlookAccount(accountCode);
      setDayFilter(undefined);
      setFocusDaily(true);
    }
    navigate("outlook");
  };
  const openAsk = (question = "") => {
    setDraft(question);
    navigate("ask");
    setTimeout(() => composer.current?.focus(), 0);
  };
  const entityMenu = (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          className="h-11 w-full justify-between bg-transparent font-normal"
          aria-label="Select entity"
        >
          <span className="truncate">{company.name}</span>
          <ChevronDown />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-56">
        {companies.map((c) => (
          <DropdownMenuItem
            key={c.key}
            onSelect={() => selectCompany(c)}
            className="min-h-11 gap-3"
          >
            <Building2 />
            {c.name}
            {c.key === company.key && <Check className="ml-auto" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
  async function ask(question: string) {
    if (!question.trim() || asking) return;
    if (new TextEncoder().encode(question).length > 16000) {
      setAskError("Please shorten your question before sending.");
      return;
    }
    const next: Message[] = [
      ...messages,
      { role: "user", content: question.trim() },
    ];
    setMessages(next);
    setDraft("");
    setAsking(true);
    setAskError("");
    const controller = new AbortController();
    askController.current = controller;
    try {
      let answer: Answer;
      if (config.demo) {
        const q = question.toLowerCase();
        if (/repair|maintenance|spending|higher|compare/.test(q))
          answer = {
            text: business
              ? "In this demo, office maintenance contributed $480 to the change in expenses. Open the operating account to inspect the supporting entry."
              : "In this demo, spending is $620 higher than at the same point last month. Home maintenance accounts for $480 of that increase, groceries $95, and dining $45. The maintenance entry is available below.",
            sources: [
              {
                label: business
                  ? "View operating activity"
                  : "View maintenance transaction",
                account: "checking",
              },
            ],
          };
        else if (/balance|cash|account|stand/.test(q))
          answer = {
            text: `This demo has ${money(sum(snapshot!.accounts.filter((a) => a.kind === "BANK").map((a) => a.balance)), company.currency)} in cash across two accounts. ${account ? account.name + " holds " + money(account.balance, company.currency) + "." : "Open an account to inspect its recorded activity."}`,
            sources: [
              {
                label: "View account activity",
                account: account?.id ?? "checking",
              },
            ],
          };
        else
          answer = {
            text: "This is a scripted design demo, so I cannot research a new question. Try “Why was spending higher?” or “How much cash do we have?” A connected AI adapter can answer your own questions using Books.",
          };
      } else
        answer = await request<Answer>("/ask", controller.signal, {
          company: company.key,
          account: account?.code,
          messages: conversationContext(
            next.map(({ role, content }) => ({ role, content })),
          ),
        });
      if (!controller.signal.aborted)
        setMessages([
          ...next,
          { role: "assistant", content: answer.text, sources: answer.sources },
        ]);
    } catch (e) {
      if (!controller.signal.aborted) {
        setAskError(
          e instanceof Error ? e.message : "Could not get an answer.",
        );
        setDraft(question);
        setMessages(next.slice(0, -1));
      }
    } finally {
      if (!controller.signal.aborted) setAsking(false);
    }
  }
  return (
    <div className="min-h-screen bg-[#fafafa]">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:bg-white focus:p-3"
      >
        Skip to content
      </a>
      <aside className="fixed inset-y-0 left-0 hidden w-60 flex-col border-r bg-[#f8f8f8] px-5 py-8 md:flex">
        <a
          href="#"
          onClick={(e) => {
            e.preventDefault();
            navigate("overview");
          }}
          className="mb-9 px-2 text-2xl font-semibold tracking-tight"
        >
          Books<span className="text-zinc-400">.</span>
        </a>
        {entityMenu}
        <nav aria-label="Main navigation" className="mt-6 space-y-1">
          {pages.map((p) => {
            const Icon = pageIcons[p];
            return (
              <Button
                key={p}
                variant="ghost"
                className={`h-11 w-full justify-start gap-3 px-3 ${current === p ? "bg-zinc-200/60 font-medium" : "font-normal text-zinc-600"}`}
                aria-current={current === p ? "page" : undefined}
                onClick={() => navigate(p)}
              >
                <Icon />
                {pageNames[p]}
              </Button>
            );
          })}
        </nav>
        <div className="mt-auto flex items-center gap-3 px-2 text-xs text-muted-foreground">
          <span className="grid size-8 place-items-center rounded-full bg-zinc-200 text-zinc-700">
            {company.name
              .split(" ")
              .map((s) => s[0])
              .slice(0, 2)
              .join("")}
          </span>
          <div>
            {company.name}
            <p className="mt-1">
              {company.currency} ·{" "}
              {config.demo ? "Demo workspace" : "Connected ledger"}
            </p>
          </div>
        </div>
      </aside>
      <div className="md:ml-60">
        <header className="flex items-center justify-between border-b bg-white px-5 py-4 md:hidden">
          <button
            onClick={() => navigate("overview")}
            className="text-xl font-semibold"
          >
            Books.
          </button>
          <Badge variant="outline">
            {config.demo ? "Demo" : company.currency}
          </Badge>
        </header>
        <div className="px-5 pt-4 md:hidden">{entityMenu}</div>
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-3 px-5 pt-3 text-xs text-muted-foreground sm:px-10 md:pt-9">
          <div className="flex items-center gap-2">
            <span
              className={`size-1.5 rounded-full ${config.demo ? "bg-amber-500" : "bg-emerald-600"}`}
            />
            {config.demo
              ? "Demo · Synthetic data · Sep 13, 2026"
              : page === "outlook"
                ? "Forecast · Estimates from a saved cash plan"
                : "Posted accounting · Source coverage may be incomplete"}
          </div>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Refresh view"
            onClick={() => {
              if (page === "outlook") return forecast.retry();
              setError("");
              if (!config.demo) setSnapshot(undefined);
              setRevision((v) => v + 1);
              if (scenario) forecast.retry();
            }}
          >
            <RefreshCw className="size-3.5" />
          </Button>
        </div>
        {!config.demo && page !== "outlook" && (
          <details className="mx-auto max-w-6xl px-5 pt-3 text-xs text-muted-foreground sm:px-10">
            <summary className="w-fit cursor-pointer py-2">
              Period: {dateLabel(range.from)} – {dateLabel(range.to)}
            </summary>
            <form
              className="flex flex-wrap items-end gap-3 rounded-lg border bg-white p-4"
              onSubmit={(e) => {
                e.preventDefault();
                const data = new FormData(e.currentTarget);
                const from = String(data.get("from")),
                  to = String(data.get("to"));
                if (from > to) {
                  setPeriodError(
                    "The start date must be on or before the end date.",
                  );
                  return;
                }
                setPeriodError("");
                resetConversation();
                setError("");
                setSnapshot(undefined);
                setRange({ from, to });
              }}
            >
              <label className="space-y-2">
                From
                <Input
                  type="date"
                  name="from"
                  aria-label="Period start"
                  defaultValue={range.from}
                  max={today()}
                  required
                  className="h-11"
                />
              </label>
              <label className="space-y-2">
                Through
                <Input
                  type="date"
                  name="to"
                  aria-label="Period end"
                  defaultValue={range.to}
                  max={today()}
                  required
                  className="h-11"
                />
              </label>
              {periodError && (
                <p role="alert" className="text-red-700">
                  {periodError}
                </p>
              )}
              <Button type="submit" className="h-11">
                Apply period
              </Button>
            </form>
          </details>
        )}
        <main
          id="main"
          className="mx-auto max-w-6xl px-5 pb-28 pt-5 sm:px-10 md:pb-10 md:pt-10"
        >
          {page === "outlook" && scenarios.length ? (
            <OutlookPage
              scenarios={scenarios}
              onScenario={setScenario}
              forecast={forecast}
              account={outlookAccount}
              onAccount={setOutlookAccount}
              filter={dayFilter}
              onFilter={setDayFilter}
              focusDaily={focusDaily}
              onFocused={() => setFocusDaily(false)}
            />
          ) : error ? (
            <div className="space-y-4 rounded-xl border bg-white p-8">
              <h1 className="text-xl font-semibold">This view couldn’t load</h1>
              <p role="alert">{error}</p>
              <Button
                onClick={() => {
                  setError("");
                  if (!config.demo) setSnapshot(undefined);
                  setRevision((v) => v + 1);
                }}
              >
                Try again
              </Button>
            </div>
          ) : !snapshot ? (
            <Loading />
          ) : page === "overview" ? (
            <>
              <div className="mb-6 sm:mb-9">
                <p className="eyebrow">
                  {business ? "Business overview" : "Financial overview"}
                </p>
                <h1 className="page-title">Your money, in view</h1>
                <p className="mt-3 text-base text-zinc-500 sm:text-lg">
                  {config.demo
                    ? "Your regular bills are covered this month."
                    : "A clear view of what’s recorded in your books."}
                </p>
              </div>
              {scenarios.length > 0 && (
                <OutlookSummary
                  forecast={forecast}
                  onOpen={() => openOutlook()}
                />
              )}
              <Card className="py-0 shadow-none">
                <CardContent className="p-5 sm:p-7">
                  <div className="flex flex-wrap justify-between gap-4">
                    <div>
                      <h2 className="text-sm font-medium">
                        {config.demo ? "Cash outlook" : "Recorded cash"}
                      </h2>
                      <p className="mt-2 text-4xl font-semibold tracking-tight sm:text-5xl">
                        {money(
                          sum(
                            snapshot.accounts
                              .filter((a) => a.kind === "BANK")
                              .map((a) => a.balance),
                          ),
                          company.currency,
                        )}
                      </p>
                      <p className="mt-2 text-xs text-muted-foreground">
                        {config.demo
                          ? "Current cash · Bank accounts"
                          : `Posted ledger balance as of ${dateLabel(snapshot.to)}`}
                      </p>
                    </div>
                    <Badge variant="outline" className="h-7">
                      {config.demo
                        ? "This month"
                        : dateLabel(snapshot.from) +
                          " – " +
                          dateLabel(snapshot.to)}
                    </Badge>
                  </div>
                  <div className="mt-5">
                    <CashChart
                      points={snapshot.points}
                      currency={company.currency}
                      cutoff={config.demo ? snapshot.to : undefined}
                    />
                  </div>
                  <div className="mt-4 flex items-center gap-5 text-xs text-muted-foreground">
                    <span className="flex items-center gap-2">
                      <span className="h-px w-5 bg-zinc-900" />
                      Recorded
                    </span>
                    {config.demo && (
                      <span className="flex items-center gap-2">
                        <span className="w-5 border-t border-dashed border-zinc-500" />
                        Projected · Demo assumptions
                      </span>
                    )}
                    <span className="ml-auto">{company.currency}</span>
                  </div>
                </CardContent>
              </Card>
              <div className="my-7 grid grid-cols-3 divide-x rounded-xl border bg-white py-5">
                {[
                  {
                    title: business ? "Revenue" : "Income",
                    value: snapshot.income,
                  },
                  { title: "Expenses", value: snapshot.expenses },
                  {
                    title: business ? "Net income" : "Net change¹",
                    value: snapshot.profit,
                  },
                ].map((m) => (
                  <div key={m.title} className="px-4 sm:px-6">
                    <p className="text-xs text-muted-foreground">{m.title}</p>
                    <p className="mt-2 break-words text-base font-semibold tracking-tight sm:text-2xl">
                      {money(m.value, company.currency)}
                    </p>
                  </div>
                ))}
              </div>
              <p className="-mt-4 mb-7 text-xs text-muted-foreground">
                {dateLabel(snapshot.from)}–{dateLabel(snapshot.to)} ·{" "}
                {company.basis} ·{" "}
                {business
                  ? "Posted performance"
                  : "¹ Income less expenses; excludes transfers and financing."}
              </p>
              <div className="mt-8 grid gap-8 lg:grid-cols-2">
                <section>
                  <h2 className="section-title">Coming up</h2>
                  {config.demo ? (
                    <div className="divide-y">
                      {[
                        {
                          name: business ? "Office rent" : "Rent",
                          date: "Sep 16",
                          amount: "180000",
                          income: false,
                        },
                        {
                          name: business ? "Client receipt" : "Paycheck",
                          date: "Sep 20",
                          amount: "320000",
                          income: true,
                        },
                      ].map((item) => (
                        <div
                          className="flex items-center gap-3 py-4"
                          key={item.name}
                        >
                          <span className="icon-tile">
                            {item.income ? <ArrowDownLeft /> : <Home />}
                          </span>
                          <span className="flex-1 text-sm">
                            {item.name}
                            <span className="mt-1 block text-xs text-muted-foreground">
                              {item.date}
                            </span>
                          </span>
                          <span
                            className={`text-sm font-medium ${item.income ? "text-emerald-700" : ""}`}
                          >
                            {item.income ? "+" : ""}
                            {money(item.amount, company.currency)}
                          </span>
                        </div>
                      ))}
                    </div>
                  ) : scenarios.length ? (
                    <div className="mt-4 rounded-lg border border-dashed p-5 text-sm leading-6 text-muted-foreground">
                      Upcoming income, bills and transfers are in Outlook, by
                      account and day.
                      <Button
                        variant="outline"
                        className="mt-3 flex h-11 bg-white"
                        onClick={() => openOutlook()}
                      >
                        Open outlook
                        <ArrowRight />
                      </Button>
                    </div>
                  ) : (
                    <p className="mt-4 rounded-lg border border-dashed p-5 text-sm leading-6 text-muted-foreground">
                      No cash plan is connected for this entity. Recorded
                      balances alone don’t establish bill coverage.
                    </p>
                  )}
                </section>
                <section>
                  <h2 className="section-title">
                    Accounts{" "}
                    <span className="ml-2 text-xs font-normal text-muted-foreground">
                      {snapshot.accounts.length}
                    </span>
                  </h2>
                  <div className="divide-y">
                    {snapshot.accounts.map((a) => (
                      <button
                        key={a.id}
                        onClick={() => openAccount(a.id)}
                        className="group flex w-full items-center gap-3 py-4 text-left"
                      >
                        <span className="icon-tile">
                          {a.kind === "BANK" ? <Landmark /> : <CreditCard />}
                        </span>
                        <span className="min-w-0 flex-1 text-sm">
                          {a.name}
                          <span className="mt-1 block text-xs text-muted-foreground">
                            {a.kind === "BANK"
                              ? "Cash account"
                              : a.kind.replaceAll("_", " ").toLowerCase()}
                          </span>
                        </span>
                        <span className="text-sm font-medium">
                          {money(a.balance, company.currency)}
                        </span>
                        <ArrowRight className="size-4 text-zinc-400 transition-transform group-hover:translate-x-1" />
                      </button>
                    ))}
                    {!snapshot.accounts.length && (
                      <p className="py-5 text-sm text-muted-foreground">
                        No financial accounts recorded yet.
                      </p>
                    )}
                  </div>
                </section>
              </div>
              {config.demo && !business && (
                <button
                  onClick={() => {
                    setDecisionChoice(plan || "later");
                    setDecision(true);
                  }}
                  className="mt-8 flex w-full items-center gap-4 rounded-xl border bg-white p-5 text-left hover:bg-zinc-50"
                >
                  <Wrench className="size-5" />
                  <span className="flex-1">
                    <span className="text-sm font-medium">
                      {plan ? "Repair plan saved" : "Planning the home repair?"}
                    </span>
                    <span className="mt-1 block text-xs text-muted-foreground">
                      {plan
                        ? (plan === "later" ? "After payday" : "This week") +
                          " · Demo plan in this browser"
                        : "See how different timing affects your checking balance."}
                    </span>
                  </span>
                  <span className="hidden text-sm font-medium sm:block">
                    {plan ? "Review plan" : "Compare timing"}
                  </span>
                  <ArrowRight className="size-4" />
                </button>
              )}
              <button
                className="mt-8 flex h-14 w-full items-center gap-3 rounded-xl border bg-white px-4 text-sm text-muted-foreground hover:border-zinc-400"
                onClick={() => openAsk()}
              >
                <Sparkles className="size-4 text-zinc-700" />
                Ask about your money…
                <ArrowUp className="ml-auto size-4" />
              </button>
              <Collapsible className="mt-6">
                <CollapsibleTrigger className="flex min-h-11 items-center gap-2 text-xs text-muted-foreground">
                  {config.demo ? "See what Books handled" : "About this view"}
                  <ChevronDown className="size-3" />
                </CollapsibleTrigger>
                <CollapsibleContent className="rounded-lg border bg-white p-4 text-sm leading-6 text-muted-foreground">
                  {config.demo
                    ? "Demo activity: statement imported, recorded transactions classified, and balances checked. These are illustrative events, not completed work on your finances."
                    : `Fetched ${new Date(snapshot.fetchedAt).toLocaleString()}. This view reads posted general-ledger and performance reports. It does not verify source completeness or include provider-pending observations. Liability balances use debit-positive ledger signs.`}
                </CollapsibleContent>
              </Collapsible>
            </>
          ) : page === "account" && account ? (
            <AccountView
              account={account}
              snapshot={snapshot}
              onBack={() => navigate("overview")}
              onAsk={() => openAsk()}
              onOutlook={
                forecast.data?.plan.accounts.some(
                  (a) => a.kind === "bank" && a.code === account.code,
                )
                  ? () => openOutlook(account.code)
                  : undefined
              }
            />
          ) : (
            <div className="mx-auto max-w-3xl">
              <p className="eyebrow">A little clarity</p>
              <h1 className="page-title">Ask Books</h1>
              <p className="mt-3 text-zinc-500">
                Get clear answers about your money.
              </p>
              <div className="mt-4 flex gap-2">
                <Badge variant="outline">{company.name}</Badge>
                {account && <Badge variant="secondary">{account.name}</Badge>}
              </div>
              {!config.demo && !config.agent && (
                <div className="mt-8 rounded-xl border bg-white p-5 text-sm leading-6">
                  <h2 className="font-medium">Connect your AI agent</h2>
                  <p className="mt-2 text-muted-foreground">
                    Your books are connected. An AI connection is needed to
                    answer questions here.
                  </p>
                </div>
              )}
              <div className="mt-8 space-y-7" aria-live="polite">
                {messages.length === 0 && (
                  <div className="py-8">
                    <span className="mb-4 grid size-10 place-items-center rounded-xl border bg-white">
                      <Sparkles className="size-5" />
                    </span>
                    <h2 className="text-xl font-medium">
                      What would you like to understand?
                    </h2>
                    <p className="mt-2 text-sm leading-6 text-muted-foreground">
                      Explore a change, inspect a transaction, or see where you
                      stand.
                    </p>
                    <div className="mt-6 flex flex-wrap gap-2">
                      {[
                        "Why was spending higher?",
                        "How much cash do we have?",
                      ].map((q) => (
                        <Button
                          key={q}
                          variant="outline"
                          className="h-11 rounded-full font-normal"
                          disabled={!config.demo && !config.agent}
                          onClick={() => ask(q)}
                        >
                          {q}
                          <ArrowUpRight />
                        </Button>
                      ))}
                    </div>
                  </div>
                )}
                {messages.map((m, i) => (
                  <div
                    key={i}
                    className={
                      m.role === "user"
                        ? "ml-auto max-w-[85%] rounded-2xl rounded-br-sm bg-zinc-200/60 px-5 py-3 text-sm"
                        : "flex gap-3"
                    }
                  >
                    {m.role === "assistant" && (
                      <span className="grid size-8 shrink-0 place-items-center rounded-full border bg-white">
                        <Sparkles className="size-4" />
                      </span>
                    )}
                    <div className="min-w-0 flex-1">
                      <p className="whitespace-pre-wrap text-sm leading-7">
                        {m.content}
                      </p>
                      {m.sources?.map((s) => (
                        <Button
                          key={s.account}
                          variant="outline"
                          className="mt-4 h-11"
                          onClick={() => openAccount(s.account)}
                        >
                          {s.label}
                          <ArrowRight />
                        </Button>
                      ))}
                      {m.role === "assistant" && (
                        <Collapsible className="mt-3">
                          <CollapsibleTrigger className="flex min-h-11 items-center gap-2 text-xs text-muted-foreground">
                            How I worked this out
                            <ChevronDown className="size-3" />
                          </CollapsibleTrigger>
                          <CollapsibleContent className="text-xs leading-6 text-muted-foreground">
                            {config.demo
                              ? "Scripted demo answer using synthetic examples as of Sep 13, 2026. No AI model or personal data was used."
                              : `Answer from your configured agent, scoped to ${company.name}${account ? ", " + account.name : ""}. Ask for supporting evidence before relying on an unsupported claim.`}
                          </CollapsibleContent>
                        </Collapsible>
                      )}
                    </div>
                  </div>
                ))}
                {asking && (
                  <p
                    role="status"
                    className="animate-pulse text-sm text-muted-foreground"
                  >
                    Books is looking into it…
                  </p>
                )}
                <div ref={end} />
              </div>
              <div className="sticky bottom-[76px] mt-8 bg-[#fafafa]/95 py-3 backdrop-blur md:bottom-0">
                {askError && (
                  <p role="alert" className="mb-3 text-sm text-red-700">
                    {askError}
                  </p>
                )}
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    ask(draft);
                  }}
                  className="flex items-end gap-2 rounded-xl border bg-white p-2 shadow-xs"
                >
                  <Textarea
                    ref={composer}
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                      if (
                        e.key === "Enter" &&
                        !e.shiftKey &&
                        !e.nativeEvent.isComposing
                      ) {
                        e.preventDefault();
                        ask(draft);
                      }
                    }}
                    disabled={!config.demo && !config.agent}
                    maxLength={8000}
                    aria-label="Ask Books a question"
                    placeholder="Ask a follow-up…"
                    className="max-h-40 min-h-11 resize-none border-0 shadow-none focus-visible:ring-0"
                  />
                  <Button
                    type="submit"
                    className="size-11 shrink-0"
                    aria-label="Send question"
                    disabled={
                      asking || !draft.trim() || (!config.demo && !config.agent)
                    }
                  >
                    <ArrowUp />
                  </Button>
                </form>
                <p className="mt-2 text-center text-[11px] text-muted-foreground">
                  {config.demo
                    ? "Scripted demo · No AI model connected"
                    : "Questions are sent to your configured AI connection."}
                </p>
              </div>
            </div>
          )}
        </main>
      </div>
      <nav
        aria-label="Mobile navigation"
        className="fixed inset-x-0 bottom-0 z-20 flex border-t bg-white/95 px-8 pb-[max(12px,env(safe-area-inset-bottom))] pt-2 backdrop-blur md:hidden"
      >
        {pages.map((p) => {
          const Icon = pageIcons[p];
          return (
            <button
              key={p}
              onClick={() => navigate(p)}
              aria-current={current === p ? "page" : undefined}
              className={`flex min-h-12 flex-1 flex-col items-center justify-center gap-1 text-[11px] ${current === p ? "font-semibold text-zinc-950" : "text-zinc-500"}`}
            >
              <Icon className="size-5" />
              {pageNames[p]}
            </button>
          );
        })}
      </nav>
      <Decision
        open={decision}
        onOpenChange={setDecision}
        company={company.key}
        choice={decisionChoice}
        onChoice={setDecisionChoice}
        onSaved={setPlan}
        onAsk={() =>
          openAsk("How would the repair affect my checking balance?")
        }
      />
    </div>
  );
}
function AccountView({
  account,
  snapshot,
  onBack,
  onAsk,
  onOutlook,
}: {
  account: Account;
  snapshot: Snapshot;
  onBack: () => void;
  onAsk: () => void;
  onOutlook?: () => void;
}) {
  const [search, setSearch] = useState("");
  const matches = account.movements
    .filter((m) =>
      `${m.description} ${m.date} ${m.amount}`
        .toLowerCase()
        .includes(search.toLowerCase()),
    )
    .toReversed();
  const actual = account.movements.filter((m) => !m.pending);
  const points = [
    { date: snapshot.from, amount: account.opening },
    ...actual.map((m) => ({ date: m.date, amount: m.balance })),
  ];
  if (points.at(-1)?.date !== snapshot.to)
    points.push({ date: snapshot.to, amount: account.balance });
  return (
    <>
      <Button
        variant="ghost"
        className="mb-5 -ml-2 h-11 text-muted-foreground"
        onClick={onBack}
      >
        <ChevronLeft />
        Overview
      </Button>
      <p className="eyebrow">Account detail</p>
      <h1 className="page-title">{account.name}</h1>
      <p className="mt-3 text-sm text-muted-foreground">
        {snapshot.demo ? "Demo bank balance" : "Posted ledger balance"} ·{" "}
        {dateLabel(snapshot.to)}
      </p>
      <p className="mb-7 mt-3 text-4xl font-semibold tracking-tight">
        {money(account.balance, snapshot.company.currency)}
      </p>
      <Card className="py-0 shadow-none">
        <CardContent className="p-5">
          <CashChart
            points={[...new Map(points.map((p) => [p.date, p])).values()]}
            currency={snapshot.company.currency}
          />
        </CardContent>
      </Card>
      <Tabs defaultValue="activity" className="mt-7">
        <TabsList variant="line">
          <TabsTrigger value="activity" className="h-11">
            Activity
          </TabsTrigger>
          <TabsTrigger value="outlook" className="h-11">
            Outlook
          </TabsTrigger>
        </TabsList>
        <TabsContent value="activity">
          <div className="relative my-4">
            <Search className="absolute left-3 top-3.5 size-4 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-11 bg-white pl-10"
              placeholder="Search transactions…"
              aria-label="Search transactions"
            />
          </div>
          <div className="divide-y rounded-xl border bg-white">
            {matches.map((m) => (
              <div key={m.id} className="flex items-center gap-3 p-4 sm:p-5">
                <span className="icon-tile">
                  {m.pending ? (
                    <Clock3 />
                  ) : BigInt(m.amount) > 0 ? (
                    <ArrowDownLeft />
                  ) : (
                    <ArrowUpRight />
                  )}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">
                    {m.description}
                  </span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {dateLabel(m.date)}
                    {m.pending
                      ? " · Pending, excluded from balance"
                      : " · Recorded"}
                  </span>
                </span>
                <span
                  className={`shrink-0 text-sm font-medium ${BigInt(m.amount) > 0 ? "text-emerald-700" : ""}`}
                >
                  {BigInt(m.amount) > 0 ? "+" : ""}
                  {money(m.amount, snapshot.company.currency)}
                </span>
              </div>
            ))}
            {!matches.length && (
              <p className="p-8 text-center text-sm text-muted-foreground">
                {search
                  ? "No transactions match your search."
                  : "No recorded activity in this date range."}
              </p>
            )}
          </div>
          <p className="mt-3 text-xs text-muted-foreground">
            {dateLabel(snapshot.from)}–{dateLabel(snapshot.to)}
            {snapshot.demo
              ? " · Synthetic activity"
              : " · Posted activity only; provider-pending transactions are not included."}
          </p>
        </TabsContent>
        <TabsContent value="outlook">
          <div className="rounded-xl border bg-white p-6">
            <h2 className="font-medium">Account outlook</h2>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              {snapshot.demo
                ? "The household overview includes an illustrative cash projection. Account forecasts are not connected."
                : onOutlook
                  ? "This account is in the saved cash plan. Outlook shows its estimated end-of-day balances, floor and any shortfall."
                  : "This account isn’t in a connected cash plan. Its recorded balance is available in Activity."}
            </p>
            {onOutlook && (
              <Button
                variant="outline"
                className="mt-4 h-11"
                onClick={onOutlook}
              >
                View daily balances in Outlook
                <ArrowRight />
              </Button>
            )}
          </div>
        </TabsContent>
      </Tabs>
      <Button
        variant="outline"
        className="mt-7 h-12 w-full justify-start gap-3 bg-white"
        onClick={onAsk}
      >
        <Sparkles />
        Ask about this account
        <ArrowRight className="ml-auto" />
      </Button>
    </>
  );
}
export default App;
