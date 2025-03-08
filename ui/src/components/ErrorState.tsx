import KagentLogo from "./kagent-logo";
import { HomeIcon } from "lucide-react";
import { Button } from "./ui/button";
import Link from "next/link";

interface ErrorStateProps {
  message: string;
  showHomeButton?: boolean;
}

export function ErrorState({ message, showHomeButton = true }: ErrorStateProps) {
  return (
    <div className="min-h-screen flex flex-col items-center justify-center p-4">
      <div className="w-full max-w-md">
        <div className="border rounded-lg p-6 w-full shadow-lg">
          <div className="flex justify-center mb-6">
            <KagentLogo className="w-24 h-auto" animate={true} />
          </div>

          <h2 className="text-red-500 font-semibold text-lg text-center py-2">Error Encountered</h2>

          <div className="border-t pt-4 mb-4">
            <p className="text-white/70 font-medium mb-2 font-mono">{message}</p>
          </div>

          {showHomeButton && (
            <Button asChild>
              <Link href="/">
                <HomeIcon className="w-4 h-4 mr-2" />
                Return to Home
              </Link>
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
