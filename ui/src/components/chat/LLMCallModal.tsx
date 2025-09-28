import React from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MessageSquare, Bot, User, Info, Terminal, Cpu, Ellipsis } from "lucide-react";
import KagentLogo from "../kagent-logo";

interface Message {
  content: string;
  role: string;
  name?: string;
}

interface LLMResponse {
  id: string;
  choices: Array<{
    message: {
      content: string;
      role: string;
    };
    finish_reason: string;
  }>;
  usage: {
    completion_tokens: number;
    prompt_tokens: number;
    total_tokens: number;
  };
  model: string;
}

export interface LLMCall {
  type: string;
  messages: Message[];
  response: LLMResponse;
  prompt_tokens: number;
  completion_tokens: number;
  agent_id?: string;
}

interface LLMCallModalProps {
  content: string;
}

const MessageBubble = ({ message }: { message: Message }) => {
  const isSystem = message.role === "system";
  const isUser = message.role === "user" && message.name === "user";
  const isAssistant = message.role === "assistant" || (message.role === "user" && message.name !== "user");

  return (
    <div className={`flex gap-3 mb-4 ${isUser ? "flex-row-reverse" : "flex-row"}`}>
      <div className="flex-shrink-0 w-8 h-8 flex items-center justify-center rounded-full bg-neutral-700">
        {isSystem ? <Info className="w-4 h-4" /> : isUser ? <User className="w-4 h-4" /> : <KagentLogo className="w-4 h-4" />}
      </div>
      <div className={`flex flex-col max-w-[80%] ${isUser ? "items-end" : "items-start"}`}>
        {message.name && <span className="text-xs text-neutral-400 mb-1">{message.name}</span>}
        <div className={`p-3 ${isSystem ? "border-l-violet-500 border-l" : isUser ? "bg-neutral-700 text-white" : isAssistant ? "border-l-violet-500 border-l" : "bg-neutral-800 text-white"}`}>
          <div className="whitespace-pre-wrap text-sm">{message.content}</div>
        </div>
      </div>
    </div>
  );
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const MetricCard = ({ icon: Icon, label, value }: { icon: any; label: string; value: string }) => (
  <div className="bg-neutral-800 p-3 rounded-lg flex items-center gap-3">
    <div className="w-8 h-8 rounded-full bg-neutral-700 flex items-center justify-center">
      <Icon className="w-4 h-4" />
    </div>
    <div>
      <div className="text-xs text-neutral-400">{label}</div>
      <div className="text-sm font-medium">{value}</div>
    </div>
  </div>
);

const LLMCallModal = ({ content }: LLMCallModalProps) => {
  const jsonObject = JSON.parse(content) as LLMCall;

  return (
    <Dialog>
      <DialogTrigger asChild>
        <div className="flex items-center gap-2 text-white/80 hover:text-violet-500 text-xs underline cursor-pointer text-left">
          <Ellipsis className="w-4 h-4" />
        </div>
      </DialogTrigger>
      <DialogContent className="bg-neutral-900 text-white border border-neutral-700 max-w-4xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle className="text-lg font-semibold flex items-center justify-between">
            <div className="flex items-center gap-2">
              <MessageSquare className="w-5 h-5" />
              LLM Call Details
            </div>
            <button onClick={() => navigator.clipboard.writeText(content)} className="text-xs text-white/50 hover:text-white underline flex items-center gap-1">
              <Terminal className="w-3 h-3" />
              Copy JSON
            </button>
          </DialogTitle>
        </DialogHeader>

        <ScrollArea className="mt-4 h-full max-h-[60vh]">
          <div className="px-4 space-y-6">
            <div className="grid grid-cols-2 gap-3">
              <MetricCard icon={Bot} label="Model" value={jsonObject.response.model} />
              <MetricCard icon={Cpu} label="Total Tokens" value={jsonObject.response.usage.total_tokens.toString()} />
            </div>

            {/* Conversation */}
            <div className="space-y-2">
              <div className="text-sm font-medium">Conversation</div>
              <div className="space-y-4">
                {jsonObject.messages.map((message, index) => (
                  <MessageBubble key={index} message={message} />
                ))}
              </div>
            </div>

            {/* Response */}
            <div className="space-y-2">
              <div className="text-sm font-medium">Response</div>
              {jsonObject.response.choices.map((choice, index) => (
                <div key={index} className="border-l border-violet-500 pl-3 py-2 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-neutral-400">Choice {index + 1}</span>
                    <span className="text-xs bg-neutral-800 px-2 py-1 rounded">{choice.finish_reason}</span>
                  </div>
                  {choice.message && (
                    <div className="text-sm">
                      <div className="bg-neutral-800 p-3 rounded">
                        <pre className="whitespace-pre-wrap text-xs">{choice.message.content}</pre>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
};

export default LLMCallModal;
