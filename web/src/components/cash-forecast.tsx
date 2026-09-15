import { useEffect, useState } from "react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ReferenceLine,
} from "recharts";
import { money, request, type Company } from "@/lib/books";
type Event = {
  id: string;
  name: string;
  date: string;
  arrival?: string;
  account: string;
  to_account?: string;
  amount: string;
  status: string;
  evidence: string;
  category?: string;
};
type Day = {
  date: string;
  account: string;
  opening: string;
  inflow: string;
  outflow: string;
  closing: string;
  floor: string;
  shortfall: string;
  movements: { event: Event; delta: string; balance: string }[];
};
type Forecast = {
  digest: string;
  scenarios: string[];
  plan: {
    name: string;
    as_of: string;
    through: string;
    currency: string;
    accounts: {
      code: string;
      name: string;
      kind: string;
      reserved: boolean;
      opening: string;
      floor: string;
      evidence: string;
    }[];
    assumptions: string[];
  };
  days: Day[];
  lows: { account: string; date: string; balance: string; shortfall: string }[];
  warnings: string[];
  variances: { expected: Event; actual: Event; amount_difference: string }[];
};
export function CashForecast({ company }: { company: Company }) {
  const [scenario, setScenario] = useState("baseline"),
    [data, setData] = useState<Forecast>(),
    [error, setError] = useState(""),
    [account, setAccount] = useState(""),
    [selected, setSelected] = useState(""),
    [scenarios, setScenarios] = useState(["baseline"]),
    [retry, setRetry] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    request<Forecast>(
      `/books/companies/${encodeURIComponent(company.key)}/cash-forecast?scenario=${encodeURIComponent(scenario)}`,
      c.signal,
    )
      .then((v) => {
        if (!c.signal.aborted) {
          setData(v);
          setScenarios(v.scenarios);
          setError("");
        }
      })
      .catch((e) => {
        if (!c.signal.aborted) {
          setError(e.message);
          setData(undefined);
        }
      });
    return () => c.abort();
  }, [company.key, scenario, retry]);
  if (error)
    return (
      <section className="mt-8 rounded-xl border p-5">
        <h2 className="section-title">Daily cash projection</h2>
        <p className="mt-2 text-sm text-muted-foreground">{error}</p>
        <label>
          Scenario{" "}
          <select
            aria-label="Scenario"
            value={scenario}
            onChange={(e) => {
              setError("");
              setScenario(e.target.value);
            }}
          >
            {scenarios.map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </label>
        <button
          className="ml-3 underline"
          onClick={() => {
            setError("");
            setScenario("baseline");
            setRetry((n) => n + 1);
          }}
        >
          Return to baseline
        </button>
      </section>
    );
  if (!data)
    return (
      <p className="mt-8 text-sm text-muted-foreground">
        Loading daily cash projection…
      </p>
    );
  const banks = data.plan.accounts.filter((a) => a.kind === "bank"),
    active = banks.find((a) => a.code === account) ?? banks[0];
  if (!active) return null;
  const rows = data.days.filter((d) => d.account === active.code),
    low = data.lows.find((l) => l.account === active.code),
    day = rows.find((d) => d.date === selected) ?? rows[0];
  const chart = rows.map((d) => ({
    date: d.date,
    balance: Number(d.closing),
    exact: d.closing,
  }));
  return (
    <section
      className="mt-8 min-w-0 rounded-xl border bg-card p-5 space-y-5"
      aria-label="Daily cash projection"
    >
      <div className="flex flex-wrap justify-between gap-3">
        <div>
          <h2 className="section-title">Daily cash projection</h2>
          <p className="text-sm text-muted-foreground">
            Opening snapshot {data.plan.as_of} · Every day through{" "}
            {data.plan.through}
          </p>
        </div>
        <label className="text-sm">
          Scenario{" "}
          <select
            className="ml-2 rounded border p-2 bg-background"
            aria-label="Scenario"
            value={scenario}
            onChange={(e) => {
              setData(undefined);
              setScenario(e.target.value);
            }}
          >
            {data.scenarios.map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </label>
      </div>
      <label className="block text-sm">
        Bank account{" "}
        <select
          aria-label="Bank account"
          className="max-w-full rounded border p-2 bg-background"
          value={active.code}
          onChange={(e) => setAccount(e.target.value)}
        >
          {banks.map((a) => (
            <option value={a.code} key={a.code}>
              {a.name}
              {a.reserved ? " · reserved" : ""}
            </option>
          ))}
        </select>
      </label>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
        <div>
          Opening
          <strong className="block text-lg">
            {money(active.opening, company.currency)}
          </strong>
        </div>
        <div>
          Lowest daily balance
          <strong className="block text-lg">
            {money(low?.balance ?? "0", company.currency)}
          </strong>
          {low?.date}
        </div>
        <div>
          Required floor
          <strong className="block text-lg">
            {money(active.floor, company.currency)}
          </strong>
        </div>
        <div>
          Shortfall at low point
          <strong className="block text-lg">
            {money(low?.shortfall ?? "0", company.currency)}
          </strong>
        </div>
      </div>
      <div
        className="h-60"
        role="img"
        aria-label={`Daily projected balances for ${active.name}`}
      >
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chart}>
            <XAxis dataKey="date" minTickGap={60} />
            <YAxis
              width={75}
              tickFormatter={(v) =>
                money(String(Math.round(v)), company.currency)
              }
            />
            <Tooltip
              formatter={(_v, _n, item) =>
                money(item.payload.exact, company.currency)
              }
            />
            <ReferenceLine
              y={Number(active.floor)}
              stroke="#d97706"
              strokeDasharray="4 4"
            />
            <Line
              type="stepAfter"
              dataKey="balance"
              stroke="#334155"
              dot={false}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <p className="text-xs text-muted-foreground">
        End-of-day balances. Intraday availability is not established. Proposed
        transfers apply only in scenarios that include them.
      </p>
      <div className="max-h-80 overflow-auto">
        <table className="w-full text-sm">
          <thead className="sticky top-0 bg-card">
            <tr>
              {["Date", "Opening", "In", "Out", "Closing", "Shortfall"].map(
                (x) => (
                  <th className="p-2 text-right first:text-left" key={x}>
                    {x}
                  </th>
                ),
              )}
            </tr>
          </thead>
          <tbody>
            {rows.map((d) => (
              <tr
                className={d.date === day?.date ? "bg-muted" : "border-t"}
                key={d.date}
              >
                <td className="p-2">
                  <button
                    className="underline"
                    onClick={() => setSelected(d.date)}
                  >
                    {d.date}
                  </button>
                </td>
                {[d.opening, d.inflow, d.outflow, d.closing, d.shortfall].map(
                  (v, i) => (
                    <td className="p-2 text-right tabular-nums" key={i}>
                      {money(v, company.currency)}
                    </td>
                  ),
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {day && (
        <div className="rounded border p-4">
          <h3 className="font-medium">{day.date} · Movements</h3>
          {day.movements.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No movements. Balance carries forward.
            </p>
          ) : (
            day.movements.map((m, i) => (
              <details className="border-t py-2 text-sm" key={m.event.id + i}>
                <summary className="cursor-pointer">
                  {m.event.name} · {money(m.delta, company.currency)} ·{" "}
                  {m.event.status}
                </summary>
                <p className="mt-2">
                  Running balance {money(m.balance, company.currency)}.{" "}
                  {m.event.category}
                </p>
                {m.event.to_account && (
                  <p>
                    Transfer{" "}
                    {
                      data.plan.accounts.find((a) => a.code === m.event.account)
                        ?.name
                    }{" "}
                    →{" "}
                    {
                      data.plan.accounts.find(
                        (a) => a.code === m.event.to_account,
                      )?.name
                    }
                    ; departure {m.event.date}, arrival {m.event.arrival}.
                  </p>
                )}
                <p className="mt-2 text-muted-foreground">
                  Evidence: {m.event.evidence}
                </p>
              </details>
            ))
          )}
        </div>
      )}
      {data.variances?.length > 0 && (
        <details className="text-sm">
          <summary>Actual versus expected</summary>
          {data.variances.map((v) => (
            <p key={v.actual.id}>
              {v.expected.name}: expected {v.expected.date}{" "}
              {money(v.expected.amount, company.currency)}; actual{" "}
              {v.actual.date} {money(v.actual.amount, company.currency)};
              difference {money(v.amount_difference, company.currency)}.
            </p>
          ))}
        </details>
      )}
      <details className="text-sm">
        <summary className="cursor-pointer">Assumptions and evidence</summary>
        <p className="mt-2">Opening balance: {active.evidence}</p>
        <ul className="list-disc pl-5">
          {[...(data.plan.assumptions ?? []), ...data.warnings].map((s, i) => (
            <li key={i}>{s}</li>
          ))}
        </ul>
        <p className="text-xs mt-2 break-all">Plan digest: {data.digest}</p>
      </details>
    </section>
  );
}
