import { useEffect, useState } from "react";
import { request, today } from "./books";
export type Bucket = {
  account: string;
  monthly: string | null;
  expense_accounts: string[];
};
export type BudgetPlan = {
  revision: string;
  buckets: Bucket[];
  assignments: { journal: string; line: number; account: string }[];
};
export type Expense = {
  journal: string;
  line: number;
  date: string;
  description: string;
  expense_account: string;
  amount: string;
  bucket: string;
  basis: string;
};
export type BudgetRow = {
  account: string;
  months: [string, string];
  average: string;
  monthly: string | null;
  count: number;
};
export type Budget = {
  can_edit: boolean;
  from: string;
  to: string;
  months: [string, string];
  rows: BudgetRow[];
  unassigned: BudgetRow;
  expenses: Expense[];
  plan: BudgetPlan;
  accounts: { code: string; name: string; type: string; subtype: string }[];
};
export function useBudget(company: string, revision?: string) {
  const [data, setData] = useState<Budget>();
  const [error, setError] = useState("");
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    if (!company) {
      setData(undefined);
      setError("");
      return;
    }
    const controller = new AbortController();
    setData(undefined);
    setError("");
    request<Budget>(
      `/books/companies/${encodeURIComponent(company)}/budget`,
      controller.signal,
      { as_of: today() },
    )
      .then(setData)
      .catch((e) => {
        if (!controller.signal.aborted) setError(e.message);
      });
    return () => controller.abort();
  }, [company, revision, refresh]);
  return {
    data,
    error,
    retry: () => setRefresh((v) => v + 1),
    save: async (plan: BudgetPlan) => {
      const result = await request<Budget>(
        `/books/companies/${encodeURIComponent(company)}/budget/save`,
        undefined,
        { as_of: today(), plan },
      );
      setData(result);
      return result;
    },
  };
}
export function decimalMinor(text: string, digits: number): string | null {
  if (!text.trim()) return null;
  if (!/^(0|[1-9]\d*)(\.\d+)?$/.test(text))
    throw new Error("Enter a nonnegative amount, such as 250.00.");
  const [whole, fraction = ""] = text.split(".");
  if (fraction.length > digits)
    throw new Error(`Use at most ${digits} decimal places.`);
  const result =
    BigInt(whole) * 10n ** BigInt(digits) +
    BigInt(fraction.padEnd(digits, "0") || "0");
  if (result > 9223372036854775807n) throw new Error("Amount is too large.");
  return result.toString();
}
export function decimalValue(minor: string | null, digits: number) {
  if (minor === null) return "";
  const n = BigInt(minor),
    unit = 10n ** BigInt(digits);
  return `${n / unit}${digits ? "." + (n % unit).toString().padStart(digits, "0") : ""}`;
}
