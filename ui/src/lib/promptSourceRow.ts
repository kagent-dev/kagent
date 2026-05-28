import { generateId } from "@/lib/utils";

export interface PromptSourceRow {
  id: string;
  name: string;
  alias: string;
}

export function newPromptSourceRow(): PromptSourceRow {
  return { id: generateId(), name: "", alias: "" };
}
