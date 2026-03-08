"use client";

import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import {
  ComposedChart,
  Line,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";

const MOCK_DATA = [
  { time: "9p", avgDuration: 42, agentRuns: 3, failedRuns: 2 },
  { time: "10p", avgDuration: 38, agentRuns: 2, failedRuns: 1 },
  { time: "11p", avgDuration: 55, agentRuns: 4, failedRuns: 3 },
  { time: "12a", avgDuration: 30, agentRuns: 1, failedRuns: 1 },
  { time: "1a", avgDuration: 25, agentRuns: 1, failedRuns: 0 },
  { time: "2a", avgDuration: 20, agentRuns: 0, failedRuns: 0 },
  { time: "3a", avgDuration: 18, agentRuns: 1, failedRuns: 1 },
  { time: "4a", avgDuration: 22, agentRuns: 0, failedRuns: 0 },
  { time: "5a", avgDuration: 35, agentRuns: 2, failedRuns: 2 },
  { time: "6a", avgDuration: 48, agentRuns: 3, failedRuns: 3 },
  { time: "7a", avgDuration: 52, agentRuns: 4, failedRuns: 4 },
  { time: "8a", avgDuration: 60, agentRuns: 5, failedRuns: 4 },
  { time: "9a", avgDuration: 65, agentRuns: 3, failedRuns: 2 },
  { time: "10a", avgDuration: 58, agentRuns: 2, failedRuns: 2 },
  { time: "11a", avgDuration: 70, agentRuns: 4, failedRuns: 3 },
  { time: "12p", avgDuration: 45, agentRuns: 2, failedRuns: 2 },
  { time: "1p", avgDuration: 50, agentRuns: 1, failedRuns: 1 },
  { time: "2p", avgDuration: 55, agentRuns: 3, failedRuns: 3 },
  { time: "3p", avgDuration: 62, agentRuns: 2, failedRuns: 2 },
  { time: "4p", avgDuration: 48, agentRuns: 1, failedRuns: 1 },
  { time: "5p", avgDuration: 40, agentRuns: 2, failedRuns: 2 },
  { time: "6p", avgDuration: 35, agentRuns: 1, failedRuns: 0 },
  { time: "7p", avgDuration: 42, agentRuns: 0, failedRuns: 0 },
  { time: "8p", avgDuration: 38, agentRuns: 1, failedRuns: 1 },
];

const TIME_RANGE_TABS = ["Avg", "P95", "1h", "24hr", "7d"];

export function ActivityChart() {
  const totalRuns = MOCK_DATA.reduce((sum, d) => sum + d.agentRuns, 0);
  const totalFailed = MOCK_DATA.reduce((sum, d) => sum + d.failedRuns, 0);
  const avgDuration = (MOCK_DATA.reduce((sum, d) => sum + d.avgDuration, 0) / MOCK_DATA.length).toFixed(1);
  const failureRate = totalRuns > 0 ? ((totalFailed / totalRuns) * 100).toFixed(1) : "0.0";

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-base">Agent Activity</CardTitle>
            <CardDescription>Runs over time with failed runs highlighted</CardDescription>
          </div>
          <div className="flex items-center gap-1 rounded-md border bg-muted p-1">
            {TIME_RANGE_TABS.map((tab) => (
              <button
                key={tab}
                className="rounded px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground data-[active=true]:bg-background data-[active=true]:text-foreground data-[active=true]:shadow-sm"
                data-active={tab === "24hr"}
              >
                {tab}
              </button>
            ))}
          </div>
        </div>
        <div className="mt-3 flex items-center gap-6 text-sm">
          <div>
            <span className="text-muted-foreground">Total runs: </span>
            <span className="font-semibold">{totalRuns}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Avg duration: </span>
            <span className="font-semibold text-cyan-500">{avgDuration}s</span>
          </div>
          <div>
            <span className="text-muted-foreground">Failed runs: </span>
            <span className="font-semibold text-red-500">{totalFailed}</span>
          </div>
          <div>
            <span className="text-muted-foreground">Failure rate: </span>
            <span className="font-semibold">{failureRate}%</span>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={320}>
          <ComposedChart data={MOCK_DATA} margin={{ top: 5, right: 20, bottom: 5, left: 0 }}>
            <XAxis
              dataKey="time"
              tick={{ fontSize: 12 }}
              tickLine={false}
              axisLine={false}
              className="text-muted-foreground"
            />
            <YAxis
              tick={{ fontSize: 12 }}
              tickLine={false}
              axisLine={false}
              className="text-muted-foreground"
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "hsl(var(--card))",
                border: "1px solid hsl(var(--border))",
                borderRadius: "var(--radius)",
                fontSize: 12,
              }}
              labelStyle={{ color: "hsl(var(--foreground))" }}
            />
            <Legend
              wrapperStyle={{ fontSize: 12 }}
            />
            <Bar
              dataKey="agentRuns"
              name="Agent Runs"
              fill="hsl(var(--chart-1, 187 70% 50%))"
              radius={[2, 2, 0, 0]}
              opacity={0.8}
            />
            <Bar
              dataKey="failedRuns"
              name="Failed Runs"
              fill="hsl(var(--destructive))"
              radius={[2, 2, 0, 0]}
              opacity={0.8}
            />
            <Line
              type="monotone"
              dataKey="avgDuration"
              name="Avg Duration (s)"
              stroke="hsl(var(--chart-2, 210 70% 60%))"
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
          </ComposedChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
