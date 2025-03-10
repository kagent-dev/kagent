import React from "react";
import { AlertTriangle, MessageSquare } from "lucide-react";
import type { ChatStatus } from "@/lib/ws";
import KagentLogo from "@/components/kagent-logo";

interface StatusDisplayProps {
  chatStatus: ChatStatus;
  errorMessage?: string;
}

export default function StatusDisplay({ chatStatus, errorMessage }: StatusDisplayProps) {
  switch (chatStatus) {
    case "ready": {
      return (
        <div className="text-xs justify-center items-center flex">
          <MessageSquare size={16} className="mr-2" />
          Ready
        </div>
      );
    }
    case "thinking": {
      return (
        <div className="text-xs justify-center items-center flex animate-pulse">
          <KagentLogo className="mr-2 w-4 h-4" />
          Thinking
        </div>
      );
    }
    case "error": {
      return (
        <div className="text-xs justify-center items-center flex">
          <AlertTriangle size={16} className="mr-2 text-red-500" />
          {errorMessage || "An error occurred"}
        </div>
      );
    }
    default: {
      return (
        <div className="text-xs justify-center items-center flex">
          <MessageSquare size={16} className="mr-2" />
          Ready
        </div>
      );
    }
  }
}
