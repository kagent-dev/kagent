import toast from "react-hot-toast";

export type SaveOutcome = "created" | "saved";

/** "Created model gpt-4o" / "Saved model gpt-4o". `kind` is lowercase. */
export function saveMessage(outcome: SaveOutcome, kind: string, name: string): string {
  return `${outcome === "created" ? "Created" : "Saved"} ${kind} ${name}`;
}

/** The success toast every create and edit form shows. */
export function toastSaved(outcome: SaveOutcome, kind: string, name: string): void {
  toast.success(saveMessage(outcome, kind, name));
}
