/**
 * Copies text, and says whether it worked.
 *
 * `navigator.clipboard` is undefined outside a secure context — an http:// host
 * that is not localhost — so the optional call it replaces was a silent no-op
 * there. The caller has to be able to tell, because the one thing worth copying
 * is a token shown once.
 *
 * The fallback is the technique antd copies with: answer the document's own
 * `copy` event rather than select a borrowed textarea. Nothing needs focus, so
 * it works inside a modal's focus trap, and success is the event having fired
 * rather than what `execCommand` claims — it returns true for copying nothing.
 * antd's own `copyable` is not used here: it ignores that result and reports a
 * copy either way.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // No API outside a secure context, or the permission was denied. Same thing
    // to the caller.
  }

  let copied = false;
  const onCopy = (event: ClipboardEvent) => {
    event.preventDefault();
    event.clipboardData?.setData("text/plain", text);
    copied = true;
  };

  try {
    document.addEventListener("copy", onCopy, { capture: true });
    document.execCommand("copy");
    return copied;
  } catch {
    return false;
  } finally {
    document.removeEventListener("copy", onCopy, { capture: true });
  }
}
