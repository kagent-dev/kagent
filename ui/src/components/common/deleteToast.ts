import toast from "react-hot-toast";

/** "Deleted model gpt-4o". `kind` is lowercase, as for `toastSaved`. */
export function deleteMessage(kind: string, name: string): string {
  return `Deleted ${kind} ${name}`;
}

/** The success toast every delete shows. */
export function toastDeleted(kind: string, name: string): void {
  toast.success(deleteMessage(kind, name));
}
