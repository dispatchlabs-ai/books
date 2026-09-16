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
import { useState } from "react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "./ui/sheet";
import {
  decimalMinor,
  decimalValue,
  type Budget,
  type BudgetPlan,
} from "@/lib/budget";
import { money, scale } from "@/lib/books";
export function BudgetEditor({
  data,
  currency,
  banks,
  save,
}: {
  data: Budget;
  currency: string;
  banks: { code: string; name: string }[];
  save: (p: BudgetPlan) => Promise<Budget>;
}) {
  const [open, setOpen] = useState(false),
    [draft, setDraft] = useState<BudgetPlan>(data.plan),
    [amounts, setAmounts] = useState<Record<string, string>>({}),
    [error, setError] = useState(""),
    [saving, setSaving] = useState(false),
    [showExpenses, setShowExpenses] = useState(false);
  const names = new Map(data.accounts.map((a) => [a.code, a.name]));
  const begin = () => {
    const p = structuredClone(data.plan);
    for (const bank of banks)
      if (!p.buckets.some((b) => b.account === bank.code))
        p.buckets.push({
          account: bank.code,
          monthly: null,
          expense_accounts: [],
        });
    setDraft(p);
    setAmounts(
      Object.fromEntries(
        p.buckets.map((b) => [
          b.account,
          decimalValue(b.monthly, scale(currency)),
        ]),
      ),
    );
    setError("");
    setOpen(true);
  };
  const commit = async () => {
    setError("");
    try {
      const p = {
        ...draft,
        buckets: draft.buckets.map((b) => ({
          ...b,
          monthly: decimalMinor(amounts[b.account] ?? "", scale(currency)),
        })),
      };
      setSaving(true);
      await save(p);
      setOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save budget.");
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <Button variant="outline" size="sm" onClick={begin}>
        Edit budgets
      </Button>
      <Sheet
        open={open}
        onOpenChange={(v) => {
          if (!saving) setOpen(v);
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-2xl w-full!">
          <SheetHeader>
            <SheetTitle>Monthly budgets</SheetTitle>
            <SheetDescription>
              Monthly spending targets. These do not change dated cash
              forecasts.
            </SheetDescription>
          </SheetHeader>
          <div className="space-y-6 p-5">
            {draft.buckets.map((b) => (
              <div
                key={b.account}
                className="flex items-center justify-between gap-4 border-b pb-4"
              >
                <label
                  className="min-w-0 text-sm font-medium wrap-anywhere"
                  htmlFor={`budget-${b.account}`}
                >
                  {names.get(b.account) ?? b.account}
                </label>
                <div className="flex shrink-0 items-center gap-2">
                  <Input
                    id={`budget-${b.account}`}
                    aria-label={`Monthly budget for ${names.get(b.account) ?? b.account}`}
                    className="w-28 text-right tabular-nums"
                    inputMode="decimal"
                    placeholder="Not set"
                    value={amounts[b.account] ?? ""}
                    disabled={saving}
                    onChange={(e) =>
                      setAmounts({ ...amounts, [b.account]: e.target.value })
                    }
                  />
                  <span className="text-xs text-muted-foreground">
                    {currency} / month
                  </span>
                </div>
              </div>
            ))}
            <Collapsible>
              <CollapsibleTrigger asChild>
                <Button variant="ghost" className="w-full justify-between px-0">
                  Spending assignments
                  <ChevronDown className="size-4" />
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent className="space-y-5 pt-4">
                <p className="text-xs text-muted-foreground">
                  Assign categories to a budget bucket. Purchase assignments
                  override categories; transfers and card payments are excluded.
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
                          draft.buckets.find((b) =>
                            b.expense_accounts.includes(a.code),
                          )?.account || "unassigned"
                        }
                        onValueChange={(value) =>
                          setDraft({
                            ...draft,
                            buckets: draft.buckets.map((b) => ({
                              ...b,
                              expense_accounts: [
                                ...b.expense_accounts.filter(
                                  (c) => c !== a.code,
                                ),
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
                <Button
                  variant="outline"
                  onClick={() => setShowExpenses((v) => !v)}
                >
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
                      <p className="my-1 font-medium">
                        {money(x.amount, currency)}
                      </p>
                      <Select
                        disabled={saving}
                        value={
                          draft.assignments.find(
                            (a) => a.journal === x.journal && a.line === x.line,
                          )?.account || "unassigned"
                        }
                        onValueChange={(value) =>
                          setDraft({
                            ...draft,
                            assignments: [
                              ...draft.assignments.filter(
                                (a) =>
                                  a.journal !== x.journal || a.line !== x.line,
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
                        Current: {names.get(x.bucket) ?? "Unassigned"} ·{" "}
                        {x.basis}
                      </p>
                    </div>
                  ))}
              </CollapsibleContent>
            </Collapsible>
            {error && (
              <p role="alert" className="text-red-700">
                {error}
              </p>
            )}
            <div className="sticky bottom-0 flex justify-end gap-2 border-t bg-white py-4">
              <Button
                variant="outline"
                disabled={saving}
                onClick={() => setOpen(false)}
              >
                Cancel
              </Button>
              <Button disabled={saving} onClick={commit}>
                {saving ? "Saving…" : "Save budgets"}
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}
