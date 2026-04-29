"use client";

import { startTransition, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Textarea } from "@/components/ui/textarea";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import { getCaretViewportCoords } from "@/lib/textareaCaret";
import { Command, CommandEmpty, CommandGroup, CommandItem, CommandList } from "@/components/ui/command";
import { listPromptTemplates } from "@/app/actions/promptTemplates";
import type { PromptTemplateSummary } from "@/types";
import {
  getActiveMention,
  matchesMentionQuery,
  scoreMentionMatch,
  TEMPLATE_VARIABLES,
  type MentionItem,
} from "@/lib/promptMentionUtils";
import { Boxes } from "lucide-react";
import { cn } from "@/lib/utils";

export type IncludePick = { configMapName: string; key: string };

export interface PromptInstructionsTextareaProps {
  id?: string;
  value: string;
  onChange: (e: React.ChangeEvent<HTMLTextAreaElement>) => void;
  onBlur?: () => void;
  error?: string;
  disabled: boolean;
  namespace: string;
  onPickInclude: (pick: IncludePick) => void;
  /**
   * Maps ConfigMap name to the first segment of `{{include "…/key"}}` (non-empty alias from the form, else the name).
   * Must match controller lookup: alias in CRD when set, otherwise ConfigMap name.
   */
  includeSourceIdForConfigMap?: (configMapName: string) => string;
  /**
   * When set, uses this catalog instead of fetching prompt libraries (Storybook/tests).
   * Omit for normal production behavior.
   */
  catalogOverride?: PromptTemplateSummary[];
}

export function PromptInstructionsTextarea({
  id: textAreaId = "agent-field-system",
  value,
  onChange,
  onBlur,
  error,
  disabled,
  namespace,
  onPickInclude,
  includeSourceIdForConfigMap,
  catalogOverride,
}: PromptInstructionsTextareaProps) {
  const taRef = useRef<HTMLTextAreaElement>(null);
  const [catalog, setCatalog] = useState<PromptTemplateSummary[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [mentionOpen, setMentionOpen] = useState(false);
  const [mentionQuery, setMentionQuery] = useState("");
  const [mentionIndex, setMentionIndex] = useState(0);
  const [mentionAnchor, setMentionAnchor] = useState<{ top: number; left: number }>({ top: 0, left: 0 });
  const mentionListRef = useRef<HTMLDivElement>(null);
  const prevMentionQueryRef = useRef<string | null>(null);

  useEffect(() => {
    const ns = namespace.trim();
    if (!ns) {
      return;
    }
    if (catalogOverride !== undefined) {
      startTransition(() => {
        setCatalog(catalogOverride);
        setCatalogLoading(false);
      });
      return;
    }
    let cancelled = false;
    void (async () => {
      startTransition(() => {
        setCatalogLoading(true);
      });
      const res = await listPromptTemplates(ns);
      if (cancelled) {
        return;
      }
      startTransition(() => {
        setCatalog(res.data ?? []);
        setCatalogLoading(false);
      });
    })();
    return () => {
      cancelled = true;
    };
  }, [namespace, catalogOverride]);

  const allMentionItems: MentionItem[] = useMemo(() => {
    const variables: MentionItem[] = TEMPLATE_VARIABLES.map((v) => ({
      kind: "variable" as const,
      insert: v.example,
      label: v.example,
      hint: v.hint,
    }));
    const out: MentionItem[] = [...variables];
    const cms = namespace.trim() ? catalog : [];
    for (const cm of cms) {
      const keys = cm.keys?.length ? cm.keys : [];
      const includeSourceId =
        includeSourceIdForConfigMap?.(cm.name)?.trim() || cm.name;
      for (const key of keys) {
        out.push({
          kind: "include",
          configMapName: cm.name,
          includeSourceId,
          key,
          label:
            includeSourceId !== cm.name
              ? `${includeSourceId} / ${key} (${cm.name})`
              : `${cm.name} / ${key}`,
        });
      }
    }
    return out;
  }, [catalog, namespace, includeSourceIdForConfigMap]);

  const filtered = useMemo(
    () => allMentionItems.filter((it) => matchesMentionQuery(it, mentionQuery)),
    [allMentionItems, mentionQuery],
  );

  const filteredSorted = useMemo(() => {
    const q = mentionQuery.trim();
    const vars = filtered.filter((it): it is Extract<MentionItem, { kind: "variable" }> => it.kind === "variable");
    const incs = filtered.filter((it): it is Extract<MentionItem, { kind: "include" }> => it.kind === "include");
    if (!q) {
      return [...vars, ...incs];
    }
    const vs = [...vars].sort((a, b) => scoreMentionMatch(b, q) - scoreMentionMatch(a, q));
    const is = [...incs].sort((a, b) => scoreMentionMatch(b, q) - scoreMentionMatch(a, q));
    return [...vs, ...is];
  }, [filtered, mentionQuery]);

  const groupedFiltered = useMemo(() => {
    const vars = filteredSorted.filter((it) => it.kind === "variable");
    const incs = filteredSorted.filter((it) => it.kind === "include");
    const groups: Array<{ key: string; heading: string; items: MentionItem[] }> = [];
    if (vars.length > 0) {
      groups.push({
        key: "__template_variables__",
        heading: "Template variables",
        items: vars,
      });
    }
    const m = new Map<string, MentionItem[]>();
    for (const it of incs) {
      const arr = m.get(it.configMapName) ?? [];
      arr.push(it);
      m.set(it.configMapName, arr);
    }
    for (const [cmName, items] of m.entries()) {
      groups.push({ key: cmName, heading: cmName, items });
    }
    return groups;
  }, [filteredSorted]);

  useEffect(() => {
    if (!mentionOpen) {
      return;
    }
    setMentionIndex((i) => Math.min(i, Math.max(0, filteredSorted.length - 1)));
  }, [filteredSorted.length, mentionOpen]);

  useLayoutEffect(() => {
    if (!mentionOpen || filteredSorted.length === 0) {
      return;
    }
    const root = mentionListRef.current;
    if (!root) {
      return;
    }
    const el = root.querySelector(`[data-mention-item-index="${mentionIndex}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [mentionIndex, mentionOpen, filteredSorted.length]);

  const updateMentionAnchor = useCallback(() => {
    const ta = taRef.current;
    if (!ta || !mentionOpen) {
      return;
    }
    const cursor = ta.selectionStart ?? ta.value.length;
    const { top, left } = getCaretViewportCoords(ta, cursor);
    setMentionAnchor({ top, left });
  }, [mentionOpen]);

  const syncMentionFromValue = useCallback(
    (text: string, cursor: number) => {
      const m = getActiveMention(text, cursor);
      if (m) {
        setMentionOpen(true);
        if (prevMentionQueryRef.current !== m.query) {
          prevMentionQueryRef.current = m.query;
          setMentionIndex(0);
        }
        setMentionQuery(m.query);
      } else {
        prevMentionQueryRef.current = null;
        setMentionOpen(false);
        setMentionQuery("");
      }
    },
    [],
  );

  useLayoutEffect(() => {
    if (!mentionOpen) {
      return;
    }
    updateMentionAnchor();
  }, [mentionOpen, value, mentionQuery, updateMentionAnchor]);

  useEffect(() => {
    if (!mentionOpen) {
      return;
    }
    const onScrollOrResize = () => updateMentionAnchor();
    window.addEventListener("scroll", onScrollOrResize, true);
    window.addEventListener("resize", onScrollOrResize);
    return () => {
      window.removeEventListener("scroll", onScrollOrResize, true);
      window.removeEventListener("resize", onScrollOrResize);
    };
  }, [mentionOpen, updateMentionAnchor]);

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    onChange(e);
    syncMentionFromValue(e.target.value, e.target.selectionStart ?? e.target.value.length);
  };

  const handleSelect = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    syncMentionFromValue(ta.value, ta.selectionStart ?? ta.value.length);
  };

  const pick = useCallback(
    (item: MentionItem) => {
      const ta = taRef.current;
      if (!ta) {
        return;
      }
      const pos = ta.selectionStart ?? value.length;
      const m = getActiveMention(value, pos);
      if (!m) {
        return;
      }
      const insert =
        item.kind === "variable"
          ? item.insert
          : `{{include "${item.includeSourceId}/${item.key}"}}`;
      const next = value.slice(0, m.start) + insert + value.slice(pos);
      onChange({ target: { value: next } } as React.ChangeEvent<HTMLTextAreaElement>);
      if (item.kind === "include") {
        onPickInclude({ configMapName: item.configMapName, key: item.key });
      }
      setMentionOpen(false);
      setMentionQuery("");
      requestAnimationFrame(() => {
        ta.focus();
        const caret = m.start + insert.length;
        ta.setSelectionRange(caret, caret);
      });
    },
    [onChange, onPickInclude, value],
  );

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Escape" && mentionOpen) {
      e.preventDefault();
      setMentionOpen(false);
      setMentionQuery("");
      return;
    }
    if (mentionOpen && filteredSorted.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setMentionIndex((i) => Math.min(i + 1, filteredSorted.length - 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setMentionIndex((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        const item = filteredSorted[mentionIndex];
        if (item) {
          pick(item);
        }
        return;
      }
    }
  };

  const pickerStatus = catalogLoading && namespace.trim()
    ? "Loading prompt libraries…"
    : "No matching items. Try another search, or add keys to a prompt library in this namespace.";

  return (
    <div className="space-y-4">
      <Popover
        open={mentionOpen}
        onOpenChange={(o) => {
          if (!o) {
            setMentionOpen(false);
            setMentionQuery("");
          }
        }}
        modal={false}
      >
        <div className="relative">
          <Textarea
            id={textAreaId}
            ref={taRef}
            value={value}
            onChange={handleChange}
            onBlur={onBlur}
            onSelect={handleSelect}
            onScroll={() => {
              if (mentionOpen) {
                requestAnimationFrame(() => updateMentionAnchor());
              }
            }}
            onKeyDown={onKeyDown}
            className={`min-h-[300px] font-mono ${error ? "border-red-500" : ""}`}
            placeholder={
              namespace.trim()
                ? `Enter instructions… Type @ for template variables or {{include "…"}} from prompt libraries in ${namespace}.`
                : "Enter instructions… Type @ to insert template variables or library keys once a namespace is set."
            }
            disabled={disabled}
            spellCheck={false}
            autoComplete="off"
            name="systemMessage"
            aria-invalid={!!error}
          />
          <PopoverAnchor asChild>
            <span
              className="pointer-events-none fixed z-50 block h-0 w-0"
              style={{ top: mentionAnchor.top, left: mentionAnchor.left }}
              aria-hidden
            />
          </PopoverAnchor>
        </div>
        <PopoverContent
          align="start"
          side="bottom"
          sideOffset={6}
          className="w-[min(100vw-2rem,24rem)] overflow-hidden rounded-xl border border-border/60 bg-popover p-0 shadow-xl ring-1 ring-black/5 dark:ring-white/10"
          onOpenAutoFocus={(ev) => ev.preventDefault()}
          onMouseDown={(ev) => ev.preventDefault()}
          aria-label="Insert prompt fragment"
        >
          <div className="border-b border-border/50 bg-muted/30 px-3 py-2.5">
            <div className="flex items-center gap-2 text-sm font-semibold tracking-tight text-foreground">
              <Boxes className="h-4 w-4 shrink-0 text-primary" aria-hidden />
              Insert prompt fragment
            </div>
            <p className="mt-0.5 text-[11px] leading-snug text-muted-foreground">
              Template variables or library keys
              {mentionQuery.trim() ? (
                <>
                  {" "}
                  · filtering by <span className="font-mono text-foreground/80">&quot;{mentionQuery}&quot;</span>
                </>
              ) : null}
              {namespace.trim() ? (
                <>
                  . Namespace <span className="font-medium text-foreground/90">{namespace}</span>
                </>
              ) : (
                <> · set a namespace to list prompt libraries</>
              )}
            </p>
            <p className="mt-2 text-[10px] leading-snug text-muted-foreground">
              Slices like <code className="rounded bg-muted/60 px-0.5 font-mono text-[10px]">{`{{range .ToolNames}}`}</code> work after tools are translated.
            </p>
          </div>
          <Command shouldFilter={false} className="bg-transparent">
            <CommandList
              ref={mentionListRef}
              className="max-h-[min(20rem,calc(100vh-10rem))] overflow-y-auto p-1.5"
            >
              <CommandEmpty className="px-3 py-6 text-center text-sm text-muted-foreground">{pickerStatus}</CommandEmpty>
              {(() => {
                let flatIndex = 0;
                return groupedFiltered.map((g) => (
                  <CommandGroup
                    key={g.key}
                    heading={
                      <span
                        className={cn(
                          "text-[11px] font-semibold normal-case tracking-normal text-foreground/85",
                          g.key === "__template_variables__" ? "font-sans" : "font-mono",
                        )}
                      >
                        {g.heading}
                      </span>
                    }
                    className="mb-2 overflow-hidden rounded-lg border border-border/45 bg-muted/20 p-0 last:mb-0 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5"
                  >
                    {g.items.map((it) => {
                      const idx = flatIndex++;
                      const isActive = idx === mentionIndex;
                      const rowKey = it.kind === "variable" ? `var:${it.insert}` : `inc:${it.configMapName}/${it.key}`;
                      return (
                        <CommandItem
                          key={rowKey}
                          data-mention-item-index={idx}
                          value={it.label}
                          onSelect={() => pick(it)}
                          onMouseEnter={() => setMentionIndex(idx)}
                          className={cn(
                            "cursor-pointer rounded-md px-2 py-2 font-sans",
                            isActive ? "bg-accent text-accent-foreground" : "aria-selected:bg-transparent",
                          )}
                        >
                          <span className="min-w-0 flex-1">
                            {it.kind === "variable" ? (
                              <>
                                <span className="block truncate font-mono text-sm font-medium text-foreground" translate="no">
                                  {it.insert}
                                </span>
                                <span className="block truncate text-[11px] text-muted-foreground">{it.hint}</span>
                              </>
                            ) : (
                              <>
                                <span className="block truncate text-sm font-medium text-foreground">{it.key}</span>
                                <span className="block truncate font-mono text-[11px] text-muted-foreground" translate="no">{`{{include "${it.includeSourceId}/${it.key}"}}`}</span>
                              </>
                            )}
                          </span>
                        </CommandItem>
                      );
                    })}
                  </CommandGroup>
                ));
              })()}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      {!namespace.trim() && (
        <p className="text-xs text-muted-foreground" role="status">
          Set an agent namespace to enable @ includes from prompt libraries in that namespace.
        </p>
      )}

      {error && (
        <p className="text-red-500 text-sm mt-1" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
