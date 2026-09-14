import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  XAxis,
  YAxis,
  Tooltip,
} from "recharts";
import { ChartContainer } from "@/components/ui/chart";
import { dateLabel, money, scale, type Point } from "@/lib/books";
export function CashChart({
  points,
  currency,
  cutoff,
}: {
  points: Point[];
  currency: string;
  cutoff?: string;
}) {
  if (
    points.some(
      (p) =>
        BigInt(p.amount) > BigInt(Number.MAX_SAFE_INTEGER) ||
        BigInt(p.amount) < BigInt(Number.MIN_SAFE_INTEGER),
    )
  )
    return (
      <p className="p-8 text-sm text-muted-foreground">
        This balance is too large to plot accurately. Exact balances are shown
        above.
      </p>
    );
  const divisor = 10 ** scale(currency);
  const boundary = points.findLastIndex((p) => !p.projected);
  // Numbers are used only for chart coordinates. Tooltips retain exact source strings.
  const data = points.map((p, i) => ({
    ...p,
    timestamp: Date.parse(p.date + "T00:00:00Z"),
    actual: !p.projected ? Number(p.amount) / divisor : null,
    forecast:
      p.projected || (i === boundary && boundary < points.length - 1)
        ? Number(p.amount) / divisor
        : null,
  }));
  return (
    <div
      role="img"
      aria-label={`Balance chart from ${dateLabel(points[0]?.date ?? "2026-01-01")} to ${dateLabel(points.at(-1)?.date ?? "2026-01-01")}. Dashed lines are projections.`}
    >
      <ChartContainer
        config={{
          actual: { label: "Recorded", color: "#18181b" },
          forecast: { label: "Projected", color: "#71717a" },
        }}
        className="h-[190px] w-full sm:h-[290px]"
      >
        <LineChart
          data={data}
          margin={{ top: 24, right: 15, left: 0, bottom: 0 }}
          accessibilityLayer
        >
          <CartesianGrid vertical={false} strokeDasharray="3 3" />
          <XAxis
            dataKey="timestamp"
            type="number"
            domain={["dataMin", "dataMax"]}
            tickFormatter={(value) =>
              dateLabel(new Date(value).toISOString().slice(0, 10))
            }
            minTickGap={32}
            axisLine={false}
            tickLine={false}
            tickMargin={14}
          />
          <YAxis
            width={55}
            axisLine={false}
            tickLine={false}
            tickFormatter={(n) =>
              Intl.NumberFormat("en-US", {
                notation: "compact",
                maximumFractionDigits: 1,
              }).format(n)
            }
          />
          <Tooltip
            content={({ active, payload }) =>
              active && payload?.[0] ? (
                <div className="rounded-lg border bg-white p-3 text-sm shadow-sm">
                  <p className="text-muted-foreground">
                    {dateLabel(payload[0].payload.date)} ·{" "}
                    {payload[0].payload.projected ? "Projected" : "Recorded"}
                  </p>
                  <strong>{money(payload[0].payload.amount, currency)}</strong>
                </div>
              ) : null
            }
          />
          {cutoff && (
            <ReferenceLine
              x={Date.parse(cutoff + "T00:00:00Z")}
              stroke="#a1a1aa"
              strokeDasharray="4 4"
              label={{
                value: "Today",
                position: "insideTopLeft",
                fill: "#71717a",
                fontSize: 11,
              }}
            />
          )}
          <Line
            type="linear"
            dataKey="actual"
            stroke="#18181b"
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
            connectNulls={false}
          />
          <Line
            type="linear"
            dataKey="forecast"
            stroke="#71717a"
            strokeWidth={2}
            strokeDasharray="5 5"
            dot={false}
            isAnimationActive={false}
            connectNulls={false}
          />
        </LineChart>
      </ChartContainer>
    </div>
  );
}
