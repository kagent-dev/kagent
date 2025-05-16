import { useState, useRef, useEffect } from "react";
import { FunctionExecutionResult, FunctionCall } from "@/types/datamodel";
import { ScrollArea } from "@radix-ui/react-scroll-area";
import { FunctionSquare, CheckCircle, Clock, Code, ChevronUp, ChevronDown, Loader2, Text, Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";

interface ToolDisplayProps {
  call: FunctionCall;
  result?: FunctionExecutionResult;
}
const ToolDisplay = ({ call, result }: ToolDisplayProps) => {
  const [areArgumentsExpanded, setAreArgumentsExpanded] = useState(false);
  const [areResultsExpanded, setAreResultsExpanded] = useState(false);
  const [isCopied, setIsCopied] = useState(false);

  const hasResult = result !== undefined;
  const [contentHeight, setContentHeight] = useState(0);
  const contentRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (contentRef.current) {
      setContentHeight(contentRef.current.scrollHeight);
    }
  }, [result]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(result?.content || "");
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), 2000);
    } catch (err) {
      console.error("Failed to copy text:", err);
    }
  };

  return (
    <Card className="w-full mx-auto my-1  min-w-full">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-xs flex space-x-5">
          <div className="flex items-center font-medium">
            <FunctionSquare className="w-4 h-4 mr-2" />
            {call.name}
          </div>
          <div className="font-light">{call.id}</div>
        </CardTitle>
        <div className="flex justify-center items-center  text-xs">
          {hasResult ? (
            <>
              <CheckCircle className="w-3 h-3 inline-block mr-2 text-green-500" />
              Call completed
            </>
          ) : (
            <>
              <Clock className="w-3 h-3 inline-block mr-2 text-yellow-500" />
              Pending
            </>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div ref={contentRef} className="space-y-2 mt-4">
          <Button variant="ghost" size="sm" className="p-0 h-auto justify-start" onClick={() => setAreArgumentsExpanded(!areArgumentsExpanded)}>
            <Code className="w-4 h-4 mr-2" />
            <span className="mr-2">Arguments</span>
            {areArgumentsExpanded ? <ChevronUp className="w-4 h-4 ml-auto" /> : <ChevronDown className="w-4 h-4 ml-auto" />}
          </Button>
          {areArgumentsExpanded && (
            <ScrollArea className="mt-2 p-4 w-full" style={{ maxHeight: `calc(80vh - ${contentHeight}px)` }}>
              <pre className="text-xs whitespace-pre-wrap break-words">{call.arguments}</pre>
            </ScrollArea>
          )}
        </div>
        <div className="mt-4 w-full">
          {!hasResult && (
            <div className="flex items-center gap-2 py-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-sm">Executing...</span>
            </div>
          )}
          {hasResult && (
            <>
              <Button variant="ghost" size="sm" className="p-0 h-auto justify-start" onClick={() => setAreResultsExpanded(!areResultsExpanded)}>
                <Text className="w-4 h-4 mr-2" />
                <span className="mr-2">Results</span>
                {areResultsExpanded ? <ChevronUp className="w-4 h-4 ml-auto" /> : <ChevronDown className="w-4 h-4 ml-auto" />}
              </Button>
              {areResultsExpanded && (
                <div className="relative">
                  <ScrollArea className="max-h-96 overflow-y-auto p-4  w-full mt-2">
                    <pre className="text-sm  whitespace-pre-wrap break-words">{result.content}</pre>
                  </ScrollArea>

                  <Button variant="ghost" size="sm" className="absolute top-2 right-2 p-2" onClick={handleCopy}>
                    {isCopied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      </CardContent>
    </Card>
  );
};

export default ToolDisplay;
