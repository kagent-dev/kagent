"use client";

import React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Plus, Trash2 } from "lucide-react";

export interface FragmentRow {
  id: string;
  key: string;
  value: string;
}

export function rowsFromData(data: Record<string, string>): FragmentRow[] {
  const keys = Object.keys(data);
  if (keys.length === 0) {
    return [{ id: crypto.randomUUID(), key: "", value: "" }];
  }
  return keys.map((key) => ({
    id: crypto.randomUUID(),
    key,
    value: data[key] ?? "",
  }));
}

export function dataFromRows(rows: FragmentRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const k = row.key.trim();
    if (k) {
      out[k] = row.value;
    }
  }
  return out;
}

export function FragmentEntriesEditor({
  rows,
  onRowsChange,
  readOnly,
  disabled,
}: {
  rows: FragmentRow[];
  onRowsChange: (rows: FragmentRow[]) => void;
  readOnly?: boolean;
  disabled?: boolean;
}) {
  const updateRow = (id: string, patch: Partial<FragmentRow>) => {
    onRowsChange(rows.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  };

  const removeRow = (id: string) => {
    const next = rows.filter((r) => r.id !== id);
    onRowsChange(next.length > 0 ? next : [{ id: crypto.randomUUID(), key: "", value: "" }]);
  };

  const addRow = () => {
    onRowsChange([...rows, { id: crypto.randomUUID(), key: "", value: "" }]);
  };

  return (
    <div className="space-y-4">
      {rows.map((row, index) => (
        <div
          key={row.id}
          className="grid gap-3 border-l-2 border-primary/35 pl-4 py-1 md:grid-cols-[minmax(0,1fr)_minmax(0,2.2fr)_auto] md:items-start"
        >
          <div className="space-y-1.5 min-w-0">
            <Label htmlFor={`frag-key-${row.id}`} className="text-xs uppercase tracking-wide text-muted-foreground">
              Key {index + 1}
            </Label>
            <Input
              id={`frag-key-${row.id}`}
              value={row.key}
              onChange={(e) => updateRow(row.id, { key: e.target.value })}
              placeholder="e.g. safety-rules…"
              disabled={disabled || readOnly}
              spellCheck={false}
              autoComplete="off"
              className="font-mono text-sm"
            />
          </div>
          <div className="space-y-1.5 min-w-0 md:col-span-1">
            <Label htmlFor={`frag-val-${row.id}`} className="text-xs uppercase tracking-wide text-muted-foreground">
              Content
            </Label>
            <Textarea
              id={`frag-val-${row.id}`}
              value={row.value}
              onChange={(e) => updateRow(row.id, { value: e.target.value })}
              placeholder="Prompt fragment text…"
              disabled={disabled || readOnly}
              className="min-h-[120px] font-mono text-sm"
            />
          </div>
          <div className="flex md:pt-7">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="shrink-0 text-muted-foreground hover:text-destructive"
              onClick={() => removeRow(row.id)}
              disabled={disabled || readOnly || rows.length <= 1}
              aria-label="Remove fragment"
            >
              <Trash2 className="h-4 w-4" aria-hidden />
            </Button>
          </div>
        </div>
      ))}
      {!readOnly && (
        <Button type="button" variant="outline" size="sm" onClick={addRow} disabled={disabled} className="gap-2">
          <Plus className="h-4 w-4" aria-hidden />
          Add fragment
        </Button>
      )}
    </div>
  );
}
