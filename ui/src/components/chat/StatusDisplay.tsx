import React from "react";
import { AlertTriangle, MessageSquare } from "lucide-react";
import KagentLogo from "@/components/kagent-logo";
import { getStatusText } from "@/lib/statusUtils";
import type { ChatStatus } from "@/types";

interface StatusDisplayProps {
  chatStatus: ChatStatus;
  errorMessage?: string;
  /** Optional progress text from WORKING status updates (e.g. CrewAI task/flow). */
  statusMessage?: string;
}

export default function StatusDisplay({ chatStatus, errorMessage, statusMessage }: StatusDisplayProps) {
  if (chatStatus === "ready") {
    return (
      <div className="text-xs justify-center items-center flex">
        <MessageSquare size={16} className="mr-2" />
        {getStatusText(chatStatus)}
      </div>
    )
  }

  if (chatStatus === "error") {
    return (
      <div className="text-xs justify-center items-center flex">
        <AlertTriangle size={16} className="mr-2 text-red-500" />
        {errorMessage || getStatusText(chatStatus)}
      </div>
    );
  }

  return (
    <div className="text-xs justify-center items-center flex animate-pulse">
      <KagentLogo className="mr-2 w-4 h-4" />
      {statusMessage || getStatusText(chatStatus)}
    </div>
  );
}
