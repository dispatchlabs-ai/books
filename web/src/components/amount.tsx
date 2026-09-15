import { cn } from "cn";
import { money, signedMoney } from "@/lib/books";

// One place decides how money reads: exact minor units, tabular figures, and
// color only for meaning (money left over, short of a floor).
export function Amount({
  value,
  currency,
  signed = false,
  tone = "none",
  className,
}: {
  value: bigint;
  currency: string;
  signed?: boolean;
  tone?: "none" | "balance" | "shortfall";
  className?: string;
}) {
  const color =
    tone === "balance"
      ? value < 0n
        ? "text-destructive"
        : value > 0n
          ? "text-positive"
          : ""
      : tone === "shortfall" && value > 0n
        ? "text-destructive"
        : "";
  return (
    <span className={cn("amount", color, className)}>
      {signed ? signedMoney(value, currency) : money(value, currency)}
    </span>
  );
}
