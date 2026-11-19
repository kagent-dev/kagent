/** Fence names that differ from Monaco's language id. */
const ALIASES: Record<string, string> = {
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  console: "shell",
  yml: "yaml",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  ts: "typescript",
  tsx: "typescript",
  py: "python",
  golang: "go",
  rs: "rust",
  rb: "ruby",
  docker: "dockerfile",
  tf: "hcl",
  terraform: "hcl",
  proto: "protobuf",
  ps1: "powershell",
  md: "markdown",
  htm: "html",
  // Monaco's JSON support is a worker-backed service; its JS tokenizer colours JSON well enough.
  json: "javascript",
  jsonc: "javascript",
  text: "plaintext",
  txt: "plaintext",
  plain: "plaintext",
};

/** Ordered guesses for an unlabelled block: the first match wins. */
const DETECTORS: [string, RegExp][] = [
  ["dockerfile", /^\s*FROM\s+\S+/m],
  [
    "shell",
    /^\s*(\$\s|#!\/.*\b(ba|z)?sh\b|(sudo\s+)?(kubectl|helm|docker|curl|git|npm|npx|yarn|go|make|export|cd|echo|kind|brew|apt(-get)?)\s)/m,
  ],
  ["go", /^\s*(package\s+\w+|func\s+(\(\w+\s+\*?\w+\)\s+)?\w+\()/m],
  ["python", /^\s*(def\s+\w+\(|from\s+[\w.]+\s+import\s|class\s+\w+(\(.*\))?:)/m],
  ["typescript", /^\s*(interface\s+\w+|type\s+\w+\s*=|(export\s+)?(const|let)\s+\w+\s*:\s*\w)/m],
  ["javascript", /^\s*((export\s+)?(const|let|var|function)\s|import\s.+\sfrom\s|module\.exports)/m],
  ["sql", /^\s*(SELECT|INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM|CREATE\s+(TABLE|INDEX))\b/im],
  ["xml", /^\s*<(\?xml|[a-zA-Z][\w-]*[\s>/])/],
  ["yaml", /^\s*(apiVersion:|kind:|---\s*$|[\w.-]+:(\s|$)|-\s+[\w.-]+:\s)/m],
];

export interface CodeLanguage {
  /** Monaco's language id. */
  id: string;
  /** What a reader calls it, shown over the block. */
  label: string;
}

/**
 * The language of a fenced block: the fence's own name when there is one,
 * otherwise a guess from the code.
 */
export function resolveCodeLanguage(fence: string | undefined, code: string): CodeLanguage {
  const named = fence?.trim().toLowerCase();
  if (named) return { id: ALIASES[named] ?? named, label: named };
  const text = code.trim();
  if (/^[[{]/.test(text) && isJson(text)) return { id: "javascript", label: "json" };
  const id = DETECTORS.find(([, pattern]) => pattern.test(text))?.[0] ?? "plaintext";
  return { id, label: id === "plaintext" ? "text" : id };
}

function isJson(text: string): boolean {
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
}
