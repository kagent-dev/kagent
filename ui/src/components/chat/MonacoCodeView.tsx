import { useMemo, type ReactNode } from "react";
import { Editor, loader } from "@monaco-editor/react";
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/basic-languages/monaco.contribution";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import type { ThemeMode } from "@/theme/theme";
import { isHexColor, type MarkdownPalette } from "./markdownPalette";
import { CODE_METRICS } from "./codeMetrics";

// The bundled Monaco and its one worker, never the loader's CDN copy.
self.MonacoEnvironment = { getWorker: () => new EditorWorker() };
loader.config({ monaco });

const OPTIONS: monaco.editor.IStandaloneEditorConstructionOptions = {
  readOnly: true,
  domReadOnly: true,
  contextmenu: false,
  links: false,
  hover: { enabled: "off" },
  minimap: { enabled: false },
  folding: false,
  glyphMargin: false,
  lineNumbers: CODE_METRICS.lineNumbers ? "on" : "off",
  lineNumbersMinChars: 3,
  lineDecorationsWidth: CODE_METRICS.lineNumbers ? 12 : CODE_METRICS.gutter,
  renderLineHighlight: "none",
  occurrencesHighlight: "off",
  selectionHighlight: false,
  matchBrackets: "never",
  guides: { indentation: false },
  stickyScroll: { enabled: false },
  overviewRulerLanes: 0,
  overviewRulerBorder: false,
  hideCursorInOverviewRuler: true,
  scrollBeyondLastLine: false,
  scrollBeyondLastColumn: 2,
  // The page scrolls past a block; only a long line scrolls it sideways.
  scrollbar: {
    vertical: "hidden",
    horizontal: "auto",
    horizontalScrollbarSize: 6,
    // The bar sits in the bottom padding, so the fixed height still fits.
    ignoreHorizontalScrollbarInContentHeight: true,
    alwaysConsumeMouseWheel: false,
    useShadows: false,
  },
  padding: { top: CODE_METRICS.padding, bottom: CODE_METRICS.padding },
  fontSize: CODE_METRICS.fontSize,
  lineHeight: CODE_METRICS.lineHeight,
  wordWrap: "off",
  automaticLayout: true,
  renderValidationDecorations: "off",
  unicodeHighlight: { ambiguousCharacters: false, invisibleCharacters: false },
};

/** Monaco token colours from the markdown palette, so code matches the prose around it. */
function defineTheme(mode: ThemeMode, palette: MarkdownPalette): string {
  const name = `kagent-markdown-${mode}`;
  const hex = (value: string) => value.replace("#", "");
  const { syntax } = palette;
  // Monaco throws on a non-hex colour (an extension may set `rgb()`), so those fall back to the base theme.
  const rules: monaco.editor.ITokenThemeRule[] = [
    { token: "", foreground: hex(palette.body) },
    { token: "keyword", foreground: hex(syntax.keyword) },
    { token: "tag", foreground: hex(syntax.keyword) },
    { token: "type", foreground: hex(syntax.key) },
    { token: "attribute.name", foreground: hex(syntax.key) },
    { token: "string", foreground: hex(syntax.string) },
    { token: "attribute.value", foreground: hex(syntax.string) },
    { token: "number", foreground: hex(syntax.number) },
    { token: "comment", foreground: hex(syntax.comment), fontStyle: "italic" },
    { token: "variable", foreground: hex(syntax.variable) },
    { token: "delimiter", foreground: hex(syntax.punctuation) },
    { token: "operator", foreground: hex(syntax.punctuation) },
    { token: "operators", foreground: hex(syntax.punctuation) },
  ];
  const colors: Record<string, string> = {
    // Transparent, so the frame's `codeBg` shows through whatever form that colour takes.
    "editor.background": "#00000000",
    "editor.foreground": palette.body,
    "editorLineNumber.foreground": palette.muted,
    "editorLineNumber.activeForeground": palette.muted,
    "editorCursor.foreground": "#00000000",
    "editor.selectionBackground": mix40(palette.link),
    "editor.inactiveSelectionBackground": mix40(palette.link),
    "scrollbarSlider.background": `${palette.muted}40`,
    "scrollbarSlider.hoverBackground": `${palette.muted}88`,
    "scrollbarSlider.activeBackground": `${palette.muted}aa`,
  };
  monaco.editor.defineTheme(name, {
    base: mode === "dark" ? "vs-dark" : "vs",
    inherit: true,
    rules: rules.filter((rule) => isHexColor(`#${rule.foreground}`)),
    colors: Object.fromEntries(Object.entries(colors).filter(([, value]) => isHexColor(value))),
  });
  return name;
}

const mix40 = (value: string) => `${value}40`;

interface MonacoCodeViewProps {
  code: string;
  language: string;
  mode: ThemeMode;
  palette: MarkdownPalette;
  fontFamily: string;
  loading: ReactNode;
}

export default function MonacoCodeView({
  code,
  language,
  mode,
  palette,
  fontFamily,
  loading,
}: MonacoCodeViewProps) {
  const theme = useMemo(() => defineTheme(mode, palette), [mode, palette]);
  const options = useMemo(() => ({ ...OPTIONS, fontFamily }), [fontFamily]);
  return (
    <Editor
      value={code}
      language={language}
      theme={theme}
      options={options}
      loading={loading}
      height="100%"
    />
  );
}
