/**
 * Copies text, and says whether it worked.
 *
 * `navigator.clipboard` is undefined outside a secure context — an http:// host
 * that is not localhost — so the optional call it replaces was a silent no-op
 * there. The caller has to be able to tell, because the one thing worth copying
 * is a token shown once.
 */
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // A denied permission looks the same to the caller as no API at all.
    }
  }

  // The pre-clipboard-API way, which needs no secure context. Deprecated, and
  // still the only thing that works when the page is not served over https.
  const area = document.createElement("textarea");
  area.value = text;
  area.style.position = "fixed";
  area.style.left = "-9999px";
  // Inside the open dialog, not on the body: a modal traps focus, so a textarea
  // outside it cannot take the selection — and `execCommand` then reports true
  // for copying nothing.
  const host = document.activeElement?.closest("[role=dialog]") ?? document.body;
  host.append(area);
  area.select();
  try {
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    area.remove();
  }
}
