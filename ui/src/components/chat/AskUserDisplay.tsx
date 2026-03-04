"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { CheckCircle, MessageSquare } from "lucide-react";
import { cn } from "@/lib/utils";

export interface AskUserQuestion {
  question: string;
  choices?: string[];
  multiple?: boolean;
}

interface AskUserDisplayProps {
  questions: AskUserQuestion[];
  onSubmit: (answers: Array<{ answer: string[] }>) => void;
  isResolved?: boolean;
  /** Resolved answers — one entry per question. */
  resolvedAnswers?: Array<{ answer: string[] }> | null;
}

/**
 * Renders the ask_user tool request as an interactive card.
 *
 * Pending state: shows each question with toggleable chips and a free-text
 *   input; a single Submit button at the bottom is enabled once every question
 *   has at least one answer.
 *
 * Resolved state: shows the same card but choices are non-interactive,
 *   selected answers are highlighted, and a green checkmark badge is shown.
 */
export default function AskUserDisplay({
  questions,
  onSubmit,
  isResolved = false,
  resolvedAnswers,
}: AskUserDisplayProps) {
  // One entry per question: set of selected choices.
  const [selectedChoices, setSelectedChoices] = useState<string[][]>(
    questions.map(() => [])
  );
  // One free-text answer per question.
  const [freeTextAnswers, setFreeTextAnswers] = useState<string[]>(
    questions.map(() => "")
  );
  const [isSubmitting, setIsSubmitting] = useState(false);

  const toggleChoice = (qIdx: number, choice: string) => {
    if (isResolved || isSubmitting) return;
    setSelectedChoices(prev => {
      const next = prev.map(s => [...s]);
      const q = questions[qIdx];
      const isMultiple = q.multiple ?? false;
      if (next[qIdx].includes(choice)) {
        next[qIdx] = next[qIdx].filter(c => c !== choice);
      } else if (isMultiple) {
        next[qIdx] = [...next[qIdx], choice];
      } else {
        // Single-select: deselect others
        next[qIdx] = [choice];
      }
      return next;
    });
  };

  const setFreeText = (qIdx: number, value: string) => {
    setFreeTextAnswers(prev => {
      const next = [...prev];
      next[qIdx] = value;
      return next;
    });
  };

  /** Whether every question has at least one selected choice or a non-empty free-text answer. */
  const isReadyToSubmit = questions.every((_, i) => {
    return selectedChoices[i].length > 0 || freeTextAnswers[i].trim().length > 0;
  });

  const handleSubmit = () => {
    if (!isReadyToSubmit || isSubmitting) return;
    setIsSubmitting(true);
    const answers = questions.map((_, i) => {
      const chips = selectedChoices[i];
      const text = freeTextAnswers[i].trim();
      // Combine chip selections and free-text into a single answer list.
      const combined = [...chips];
      if (text && !combined.includes(text)) {
        combined.push(text);
      }
      return { answer: combined.length > 0 ? combined : [text] };
    });
    onSubmit(answers);
  };

  // Use resolved answers when in resolved state
  const displayAnswers = isResolved && resolvedAnswers ? resolvedAnswers : null;

  return (
    <Card className={cn("w-full mx-auto my-1 min-w-full", isResolved ? "border-green-300 dark:border-green-700" : "border-blue-300 dark:border-blue-700")}>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-xs flex items-center gap-2">
          <MessageSquare className="w-4 h-4" />
          <span className="font-medium">Questions for you</span>
        </CardTitle>
        {isResolved && (
          <div className="flex items-center text-xs text-green-600 dark:text-green-400 gap-1">
            <CheckCircle className="w-3 h-3" />
            Answered
          </div>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {questions.map((q, qIdx) => {
          const answered = displayAnswers?.[qIdx]?.answer ?? [];
          return (
            <div key={qIdx} className="space-y-2">
              <p className="text-sm font-medium">{q.question}</p>

              {/* Choice chips */}
              {q.choices && q.choices.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {q.choices.map((choice) => {
                    const isSelected = isResolved
                      ? answered.includes(choice)
                      : selectedChoices[qIdx].includes(choice);
                    return (
                      <button
                        key={choice}
                        type="button"
                        disabled={isResolved || isSubmitting}
                        onClick={() => toggleChoice(qIdx, choice)}
                        className={cn(
                          "px-3 py-1 rounded-full text-xs border transition-colors",
                          isSelected
                            ? "bg-primary text-primary-foreground border-primary"
                            : "bg-muted text-muted-foreground border-border hover:border-primary hover:text-primary",
                          (isResolved || isSubmitting) && "cursor-default opacity-80"
                        )}
                      >
                        {choice}
                      </button>
                    );
                  })}
                </div>
              )}

              {/* Free-text input — always shown when pending, shown read-only when resolved */}
              {isResolved ? (
                /* Resolved: show the free-text portion of the answer (non-chip answers) */
                (() => {
                  const chipAnswers = q.choices ?? [];
                  const freeAnswers = answered.filter(a => !chipAnswers.includes(a));
                  return freeAnswers.length > 0 ? (
                    <p className="text-xs text-muted-foreground border rounded p-2 bg-muted/50">
                      {freeAnswers.join(", ")}
                    </p>
                  ) : null;
                })()
              ) : (
                <Input
                  value={freeTextAnswers[qIdx]}
                  onChange={(e) => setFreeText(qIdx, e.target.value)}
                  placeholder="Type your own answer"
                  disabled={isSubmitting}
                  className="text-sm"
                />
              )}
            </div>
          );
        })}

        {!isResolved && (
          <Button
            size="sm"
            variant="default"
            disabled={!isReadyToSubmit || isSubmitting}
            onClick={handleSubmit}
            className="mt-2"
          >
            {isSubmitting ? "Submitting…" : "Submit"}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
