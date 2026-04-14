import { Textarea } from "@/components/ui/textarea";
import { PromptInstructionsTextarea, type IncludePick } from "@/components/create/PromptInstructionsTextarea";
import type { PromptTemplateSummary } from "@/types";

interface SystemPromptSectionProps {
  value: string;
  onChange: (e: React.ChangeEvent<HTMLTextAreaElement>) => void;
  onBlur?: () => void;
  error?: string;
  disabled: boolean;
  /** When set with onPickInclude, typing @ opens a fragment picker (prompt libraries in the namespace). */
  mentionNamespace?: string;
  onPickInclude?: (pick: IncludePick) => void;
  /** Maps ConfigMap name to include path prefix (alias when set on a prompt source row, else name). */
  includeSourceIdForConfigMap?: (configMapName: string) => string;
  /** Passed through to PromptInstructionsTextarea for Storybook/tests. */
  promptLibraryCatalogOverride?: PromptTemplateSummary[];
}

export const SystemPromptSection = ({
  value,
  onChange,
  onBlur,
  error,
  disabled,
  mentionNamespace,
  onPickInclude,
  includeSourceIdForConfigMap,
  promptLibraryCatalogOverride,
}: SystemPromptSectionProps) => {
  const useMentions = Boolean(mentionNamespace?.trim() && onPickInclude);

  return (
    <div>
      <label className="text-base mb-2 block font-bold">Agent Instructions</label>
      <p className="text-xs mb-2 block text-muted-foreground">
        These instructions define the agent&apos;s behavior. They are processed as a Go template when you include
        prompt fragments from prompt libraries.{" "}
        {useMentions ? (
          <>
            Type <kbd className="rounded border bg-muted px-1 font-mono text-[11px]">@</kbd> to insert an include and
            register that library as a prompt source.
          </>
        ) : (
          "Set an agent namespace to enable @ includes from prompt libraries in that namespace."
        )}
      </p>
      <div className="space-y-4">
        {useMentions ? (
          <PromptInstructionsTextarea
            value={value}
            onChange={onChange}
            onBlur={onBlur}
            error={error}
            disabled={disabled}
            namespace={mentionNamespace!.trim()}
            onPickInclude={onPickInclude!}
            includeSourceIdForConfigMap={includeSourceIdForConfigMap}
            catalogOverride={promptLibraryCatalogOverride}
          />
        ) : (
          <>
            <Textarea
              value={value}
              onChange={onChange}
              onBlur={onBlur}
              className={`min-h-[300px] font-mono ${error ? "border-red-500" : ""}`}
              placeholder="Enter the agent instructions. These instructions tell the agent how to behave and what to do."
              disabled={disabled}
              spellCheck={false}
              autoComplete="off"
              name="systemMessage"
            />
            {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
          </>
        )}
      </div>
    </div>
  );
};
