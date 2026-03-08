"use client";

import { useEffect, useState } from "react";
import { getDashboardStats } from "@/app/actions/dashboard";
import { DashboardStatsResponse } from "@/types";
import { DashboardTopBar } from "@/components/dashboard/DashboardTopBar";
import { StatsRow } from "@/components/dashboard/StatsRow";
import { ActivityChart } from "@/components/dashboard/ActivityChart";
import { RecentRunsPanel } from "@/components/dashboard/RecentRunsPanel";
import { LiveFeedPanel } from "@/components/dashboard/LiveFeedPanel";
import { Skeleton } from "@/components/ui/skeleton";

export default function DashboardPage() {
  const [stats, setStats] = useState<DashboardStatsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchStats = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getDashboardStats();
      setStats(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load dashboard");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStats();
  }, []);

  if (error) {
    return (
      <div className="space-y-6 p-6">
        <DashboardTopBar />
        <div className="flex flex-col items-center justify-center gap-4 py-12">
          <p className="text-sm text-destructive">{error}</p>
          <button onClick={fetchStats} className="text-sm text-primary hover:underline">
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      <DashboardTopBar />
      <div>
        <h1 className="text-3xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground">Overview of your KAgent cluster</p>
      </div>

      {loading ? (
        <div className="space-y-6">
          <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-4">
            {Array.from({ length: 7 }).map((_, i) => (
              <Skeleton key={i} className="h-20" />
            ))}
          </div>
          <Skeleton className="h-[400px]" />
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <Skeleton className="h-[350px]" />
            <Skeleton className="h-[350px]" />
          </div>
        </div>
      ) : stats ? (
        <>
          <StatsRow counts={stats.counts} />
          <ActivityChart />
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <RecentRunsPanel runs={stats.recentRuns} />
            <LiveFeedPanel events={stats.recentEvents} />
          </div>
        </>
      ) : null}
    </div>
  );
}
