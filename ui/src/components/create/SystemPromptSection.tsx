import { Textarea } from "@/components/ui/textarea";

interface SystemPromptSectionProps {
  value: string;
  onChange: (e: React.ChangeEvent<HTMLTextAreaElement>) => void;
  onBlur?: () => void;
  error?: string;
  disabled: boolean;
}

export const SystemPromptSection = ({ value, onChange, onBlur, error, disabled }: SystemPromptSectionProps) => {
  return (
    <div>
      <label className="text-base mb-2 block font-bold">Agent Instructions</label>
      <p className="text-xs mb-2 block text-muted-foreground">
        These instructions tell the agent how to behave and what to do. We provided a default set of instructions, but you can customize them to your needs.
      </p>
      <div className="space-y-4">
        <Textarea
          value={value}
          onChange={onChange}
          onBlur={onBlur}
          className={`min-h-[300px] font-mono ${error ? "border-red-500" : ""}`}
          placeholder="Enter the agent instructions. These instructions tell the agent how to behave and what to do."
          disabled={disabled}
        />

        {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
      </div>
    </div>
  );
};
