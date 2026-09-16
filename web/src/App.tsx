import { useEffect, useState } from "react";
import { Check, ChevronDown, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Cash } from "@/components/cash";
import {
  today,
  loadSnapshot,
  request,
  type Company,
  type Snapshot,
} from "@/lib/books";
import { demoCompanies, demoSnapshot } from "@/lib/demo";
import { useForecast } from "@/lib/use-forecast";

type Config = {
  session?: boolean;
  demo: boolean;
  forecasts?: Record<string, string[]>;
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
  if (error || (config && !company))
    return (
      <main className="mx-auto max-w-md space-y-4 px-6 py-24">
        <h1 className="text-2xl font-semibold">
          {error ? "Books unavailable" : "No entities available"}
        </h1>
        <p role="alert" className="text-muted-foreground">
          {error || "Your Books connection has no accessible entities."}
        </p>
        <Button
          onClick={() => {
            setError("");
            setRetry((v) => v + 1);
          }}
        >
          Try again
        </Button>
      </main>
    );
  if (!config || !company)
    return (
      <main
        className="mx-auto max-w-6xl space-y-6 p-8"
        role="status"
        aria-label="Loading Books"
      >
        <Skeleton className="h-9 w-40" />
        <Skeleton className="h-80 w-full" />
        <span className="sr-only">Loading Books</span>
      </main>
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
  const scenario = config.demo
    ? ""
    : (config.forecasts?.[company.key]?.[0] ?? "");
  const forecast = useForecast(company.key, scenario);
  useEffect(() => {
    const controller = new AbortController();
    if (!config.demo)
      loadSnapshot(
        company,
        controller.signal,
        {
          from: today().slice(0, 7) + "-01",
          to: today(),
        },
        forecast.data?.plan.accounts
          .filter((a) => a.kind === "bank")
          .map((a) => a.code),
      )
        .then((s) => {
          if (!controller.signal.aborted) {
            setSnapshot(s);
            setError("");
          }
        })
        .catch((e) => {
          if (!controller.signal.aborted) setError(e.message);
        });
    return () => controller.abort();
  }, [company, config.demo, revision, forecast.data]);
  const refresh = () => {
    setError("");
    if (!config.demo) setSnapshot(undefined);
    setRevision((v) => v + 1);
    if (scenario) forecast.retry();
  };
  return (
    <div className="min-h-screen">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:bg-white focus:p-3"
      >
        Skip to content
      </a>
      <header className="app-header">
        <div className="app-header-inner">
          <a href="#main" className="wordmark">
            Books
          </a>
          <span className="header-divider" aria-hidden="true" />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="entity-switcher"
                aria-label={`Select entity, current: ${company.name}`}
              >
                <span className="truncate">{company.name}</span>
                <ChevronDown className="size-3.5 text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="min-w-56">
              {companies.map((c) => (
                <DropdownMenuItem
                  key={c.key}
                  onSelect={() => selectCompany(c)}
                  className="min-h-10"
                >
                  {c.name}
                  {c.key === company.key && <Check className="ml-auto" />}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {config.demo && (
              <span
                className="mr-2 text-xs text-muted-foreground"
                title="Synthetic data"
              >
                Demo
              </span>
            )}
            <Button
              variant="ghost"
              size="icon"
              aria-label="Refresh view"
              title="Refresh view"
              onClick={refresh}
            >
              <RefreshCw className="size-4" />
            </Button>
            {config.session && (
              <form action="/logout" method="post">
                <Button
                  variant="ghost"
                  size="sm"
                  type="submit"
                  className="text-muted-foreground"
                >
                  Sign out
                </Button>
              </form>
            )}
          </div>
        </div>
      </header>
      <main id="main" className="cash-workspace">
        <Cash
          company={config.demo ? "" : company.key}
          currency={company.currency}
          forecast={forecast}
          snapshot={snapshot}
          ledgerError={error}
          retryLedger={refresh}
        />
      </main>
    </div>
  );
}
export default App;
