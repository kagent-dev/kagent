import { GitFork } from "lucide-react";

export default function GitReposPage() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[400px] gap-4 text-muted-foreground">
      <GitFork className="h-12 w-12 opacity-30" />
      <p className="text-lg font-medium">GIT Repos</p>
      <p className="text-sm">Coming soon</p>
    </div>
  );
}
