"use client";

import KagentLogo from "./kagent-logo";
import { HomeIcon } from "lucide-react";

interface ErrorStateProps {
  message: string;
  showHomeButton?: boolean;
}

export function ErrorState({ message, showHomeButton = true }: ErrorStateProps) {
  return (
    <div className="min-h-screen flex flex-col items-center justify-center bg-[#1A1A1A] p-4">
      <div className="w-full max-w-md">
        <div className="bg-[#2A2A2A] border border-[#3A3A3A] rounded-lg p-6 w-full shadow-lg">
          <div className="flex justify-center mb-6">
            <KagentLogo className="w-24 h-auto" animate={true} />
          </div>

          <h2 className="text-red-500 font-semibold text-lg text-center py-2">Error Encountered</h2>

          <div className="border-t border-[#3A3A3A] pt-4 mb-4">
            <p className="text-white/70 font-medium mb-2 font-mono">{message}</p>
          </div>

          {showHomeButton && (
            <button
              onClick={() => (window.location.href = "/")}
              className="w-full py-2 px-4 bg-[#942DE7] hover:bg-[#8129CC] text-white rounded-md transition-colors duration-200 flex items-center justify-center"
            >
              <HomeIcon className="w-4 h-4 mr-2"/>
              Return to Home
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
