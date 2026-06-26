// Sticky bottom composer for the ACP harness chat: status line, the prompt
// textarea (Enter to send, Shift+Enter for newline), the Send button, and a
// Cancel action while a turn is running. Dropped sockets reconnect
// automatically (the harness actor resumes on demand), so there's no manual
// Reconnect action.

import type React from "react";
import { ArrowBigUp, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import StatusDisplay from "./StatusDisplay";
import { getStatusPlaceholder } from "@/lib/statusUtils";
import type { ChatStatus } from "@/types";

interface AcpChatComposerProps {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  chatStatus: ChatStatus;
  /** Show the Cancel button (a turn is running). */
  showCancel: boolean;
  onCancel: () => void;
}

export default function AcpChatComposer({
  value,
  onChange,
  onSubmit,
  chatStatus,
  showCancel,
  onCancel,
}: AcpChatComposerProps) {
  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      e.currentTarget.form?.requestSubmit();
    }
  };

  return (
    <div className="w-full shrink-0 sticky bg-secondary bottom-0 md:bottom-2 rounded-none md:rounded-lg p-4 border overflow-hidden transition-all duration-300 ease-in-out">
      <div className="flex items-center justify-between mb-4 gap-2">
        <StatusDisplay chatStatus={chatStatus} />
      </div>

      <form onSubmit={onSubmit}>
        <Textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={getStatusPlaceholder(chatStatus)}
          onKeyDown={handleKeyDown}
          className={`min-h-[100px] border-0 shadow-none p-0 focus-visible:ring-0 resize-none ${chatStatus !== "ready" ? "opacity-50 cursor-not-allowed" : ""}`}
          disabled={chatStatus !== "ready"}
        />

        <div className="flex items-center justify-end gap-2 mt-4">
          <Button type="submit" disabled={!value.trim() || chatStatus !== "ready"}>
            Send
            <ArrowBigUp className="h-4 w-4 ml-2" />
          </Button>
          {showCancel && (
            <Button type="button" variant="outline" onClick={onCancel}>
              <X className="h-4 w-4 mr-2" /> Cancel
            </Button>
          )}
        </div>
      </form>
    </div>
  );
}
