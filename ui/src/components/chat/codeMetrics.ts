/** Code block type metrics, shared by the Monaco view and its same-sized placeholder. */
export const CODE_METRICS = {
  fontSize: 13,
  lineHeight: 20,
  padding: 12,
  // Monaco's left gap before the text, with line numbers off.
  gutter: 16,
  lineNumbers: false,
} as const;
