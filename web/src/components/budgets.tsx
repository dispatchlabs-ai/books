import { useState } from "react";
import { ChevronDown } from "lucide-react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "./ui/collapsible";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./ui/select";
import { Button } from "./ui/button";
import type { Budget, BudgetPlan } from "@/lib/budget";
import { money } from "@/lib/books";

export function BudgetAssignments({
  data,
  currency,
  draft,
  onChange,
  saving,
}: {
  data: Budget;
  currency: string;
  draft: BudgetPlan;
  onChange: (plan: BudgetPlan) => void;
  saving: boolean;
}) {
  const [showExpenses, setShowExpenses] = useState(false);
  const names = new Map(data.accounts.map((a) => [a.code, a.name]));
  return (
    <Collapsible>
      <CollapsibleTrigger asChild>
        <Button variant="ghost" className="w-full justify-between px-0">
          Spending assignments
          <ChevronDown className="size-4" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-5 pt-4">
        <p className="text-xs text-muted-foreground">
          Assign categories to a budget bucket. Purchase assignments override
          categories; transfers and card payments are excluded.
        </p>
        <h3 className="text-sm font-medium">Categories</h3>
        {data.accounts
          .filter((a) => a.type === "EXPENSE")
          .map((a) => (
            <label
              key={a.code}
              className="flex flex-wrap items-center justify-between gap-2 text-sm"
            >
              <span>{a.name}</span>
              <Select
                disabled={saving}
                value={
                  draft.buckets.find((b) => b.expense_accounts.includes(a.code))
                    ?.account || "unassigned"
                }
                onValueChange={(value) =>
                  onChange({
                    ...draft,
                    buckets: draft.buckets.map((b) => ({
                      ...b,
                      expense_accounts: [
                        ...b.expense_accounts.filter((c) => c !== a.code),
                        ...(b.account === value ? [a.code] : []),
                      ],
                    })),
                  })
                }
              >
                <SelectTrigger
                  aria-label={`Bucket for ${a.name}`}
                  className="max-w-full sm:max-w-72"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="unassigned">
                    Direct bank / unassigned
                  </SelectItem>
                  {draft.buckets.map((b) => (
                    <SelectItem key={b.account} value={b.account}>
                      {names.get(b.account) ?? b.account}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
          ))}
        <Button variant="outline" onClick={() => setShowExpenses((v) => !v)}>
          {showExpenses ? "Hide" : "Review"} purchase assignments (
          {data.expenses.length})
        </Button>
        {showExpenses &&
          data.expenses.map((x) => (
            <div
              key={`${x.journal}:${x.line}`}
              className="rounded border p-3 text-sm"
            >
              <p>
                {x.date} · {x.description}
              </p>
              <p className="my-1 font-medium">{money(x.amount, currency)}</p>
              <Select
                disabled={saving}
                value={
                  draft.assignments.find(
                    (a) => a.journal === x.journal && a.line === x.line,
                  )?.account || "unassigned"
                }
                onValueChange={(value) =>
                  onChange({
                    ...draft,
                    assignments: [
                      ...draft.assignments.filter(
                        (a) => a.journal !== x.journal || a.line !== x.line,
                      ),
                      ...(value !== "unassigned"
                        ? [
                            {
                              journal: x.journal,
                              line: x.line,
                              account: value,
                            },
                          ]
                        : []),
                    ],
                  })
                }
              >
                <SelectTrigger
                  aria-label={`Bucket for ${x.description} ${x.date} line ${x.line}`}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="unassigned">
                    Use category / direct bank
                  </SelectItem>
                  {draft.buckets.map((b) => (
                    <SelectItem key={b.account} value={b.account}>
                      {names.get(b.account) ?? b.account}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="mt-1 text-xs text-muted-foreground">
                Current: {names.get(x.bucket) ?? "Unassigned"} · {x.basis}
              </p>
            </div>
          ))}
      </CollapsibleContent>
    </Collapsible>
  );
}
