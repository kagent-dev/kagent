export interface PromptSourceRow {
  id: string;
  name: string;
  alias: string;
}

export function newPromptSourceRow(): PromptSourceRow {
  return { id: crypto.randomUUID(), name: "", alias: "" };
}
