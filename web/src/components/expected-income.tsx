import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { expectedIncome, type Horizon } from "@/lib/cash";
import { dateLabel, money, rangeLabel } from "@/lib/books";
import type { ForecastState } from "@/lib/use-forecast";

export function ExpectedIncome({
  forecast,
  horizon,
  today,
  currency,
}: {
  forecast: ForecastState;
  horizon: Horizon;
  today: string;
  currency: string;
}) {
  const income =
    forecast.data && !forecast.error && !forecast.loading
      ? expectedIncome(forecast.data, horizon, today)
      : undefined;
  const unavailable = forecast.error
    ? "Unavailable"
    : forecast.scenario && forecast.loading
      ? "Loading…"
      : "Not forecast";
  return (
    <Collapsible className="expected-income" aria-label="Expected income">
      <div className="income-summary">
        <CollapsibleTrigger asChild>
          <Button
            variant="ghost"
            className="income-trigger"
            disabled={!income?.receipts.length}
          >
            <span>Expected income</span>
            <strong className="amount">
              {income ? money(income.total, currency) : unavailable}
            </strong>
            {!!income?.receipts.length && (
              <ChevronDown aria-hidden="true" className="size-3.5" />
            )}
          </Button>
        </CollapsibleTrigger>
        {income && !income.complete && (
          <span className="income-coverage">
            {rangeLabel(income.from, income.through)} only
          </span>
        )}
      </div>
      {income && (
        <CollapsibleContent>
          <ul className="income-receipts" aria-label="Expected payments">
            {income.receipts.map(({ event, account }) => (
              <li key={event.id}>
                <time dateTime={event.date}>{dateLabel(event.date)}</time>
                <div className="income-description">
                  <span>{event.name}</span>
                  <small>{account.name}</small>
                </div>
                <div className="income-amount">
                  <span className="amount">
                    {money(event.amount, currency)}
                  </span>
                  <small>
                    {event.status === "actual"
                      ? "Recorded"
                      : event.status[0].toUpperCase() + event.status.slice(1)}
                  </small>
                </div>
              </li>
            ))}
          </ul>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}
