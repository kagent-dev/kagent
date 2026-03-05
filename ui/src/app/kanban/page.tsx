"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import type { LucideIcon } from "lucide-react";
import { CheckCircle2, FlaskConical, GitPullRequest, Inbox, Lightbulb, Paperclip, Rocket, Wrench } from "lucide-react";

const KANBAN_BASE_URL = "/kanban-mcp/";
const kanbanUrl = (path: string) =>
  `${KANBAN_BASE_URL.replace(/\/+$/, "")}/${path.replace(/^\/+/, "")}`;
const WORKFLOW = ["Inbox", "Design", "Develop", "Testing", "SecurityScan", "CodeReview", "Documentation", "Release", "Done"] as const;

const STAGE_META: Record<TaskStatus, { label: string; icon: LucideIcon }> = {
  Inbox: { label: "Inbox", icon: Inbox },
  Design: { label: "Design", icon: Lightbulb },
  Develop: { label: "Develop", icon: Wrench },
  Testing: { label: "Testing", icon: FlaskConical },
  SecurityScan: { label: "Security Scan", icon: FlaskConical },
  CodeReview: { label: "Code Review", icon: GitPullRequest },
  Documentation: { label: "Documentation", icon: Paperclip },
  Release: { label: "Release", icon: Rocket },
  Done: { label: "Done", icon: CheckCircle2 },
};

type TaskStatus = (typeof WORKFLOW)[number];

interface RawTask {
  id?: number;
  ID?: number;
  title?: string;
  Title?: string;
  description?: string;
  Description?: string;
  status?: string;
  Status?: string;
  assignee?: string;
  Assignee?: string;
  user_input_needed?: boolean;
  UserInputNeeded?: boolean;
  parent_id?: number | null;
  ParentID?: number | null;
  attachments?: RawAttachment[];
  Attachments?: RawAttachment[];
  subtasks?: RawTask[];
  Subtasks?: RawTask[];
}

interface RawAttachment {
  id?: number;
  ID?: number;
}

interface Task {
  id: number;
  title: string;
  description: string;
  status: string;
  assignee: string;
  user_input_needed: boolean;
  parent_id: number | null;
  attachments: Attachment[];
  subtasks: Task[];
}

interface Attachment {
  id: number;
}

interface BoardColumn {
  status: string;
  tasks: RawTask[];
}

interface BoardData {
  columns: BoardColumn[];
}

interface SseEnvelope {
  type?: string;
  data?: BoardData;
}

function isBoardData(value: unknown): value is BoardData {
  if (!value || typeof value !== "object") {
    return false;
  }
  return Array.isArray((value as BoardData).columns);
}

function normalizeTask(task: RawTask): Task {
  return {
    id: Number(task.id ?? task.ID ?? 0),
    title: task.title ?? task.Title ?? "",
    description: task.description ?? task.Description ?? "",
    status: task.status ?? task.Status ?? "",
    assignee: task.assignee ?? task.Assignee ?? "",
    user_input_needed: Boolean(task.user_input_needed ?? task.UserInputNeeded),
    parent_id: task.parent_id ?? task.ParentID ?? null,
    attachments: (task.attachments ?? task.Attachments ?? []).map((attachment) => ({
      id: Number(attachment.id ?? attachment.ID ?? 0),
    })),
    subtasks: (task.subtasks ?? task.Subtasks ?? []).map(normalizeTask),
  };
}

function normalizeBoard(board: BoardData | null): Record<string, Task[]> {
  const normalized: Record<string, Task[]> = {};
  WORKFLOW.forEach((status) => {
    normalized[status] = [];
  });

  if (!board?.columns) {
    return normalized;
  }

  for (const column of board.columns) {
    if (column.status in normalized) {
      normalized[column.status] = (column.tasks ?? []).map(normalizeTask);
    }
  }

  return normalized;
}

export default function KanbanPage() {
  const [board, setBoard] = useState<Record<string, Task[]>>(() => normalizeBoard(null));
  const [isLive, setIsLive] = useState(false);
  const [inboxTitle, setInboxTitle] = useState("");

  const applyBoard = useCallback((payload: BoardData | null | undefined) => {
    if (!payload) return;
    setBoard(normalizeBoard(payload));
  }, []);

  const fetchBoard = useCallback(async () => {
    try {
      const response = await fetch(kanbanUrl("/api/board"));
      if (!response.ok) {
        throw new Error(`Board fetch failed: ${response.status}`);
      }
      const data = (await response.json()) as BoardData;
      applyBoard(data);
    } catch (error) {
      console.error("fetchBoard:", error);
    }
  }, [applyBoard]);

  const updateTask = useCallback(async (id: number, patch: Record<string, unknown>) => {
    try {
      await fetch(kanbanUrl(`/api/tasks/${id}`), {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      });
    } catch (error) {
      console.error("updateTask:", error);
    }
  }, []);

  const addTask = useCallback(
    async (title: string) => {
      try {
        await fetch(kanbanUrl("/api/tasks"), {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ title, status: "Inbox" }),
        });
      } catch (error) {
        console.error("addTask:", error);
      }
    },
    [],
  );

  useEffect(() => {
    fetchBoard();

    const events = new EventSource(kanbanUrl("/events"));

    const applyEventData = (raw: string) => {
      try {
        const parsed = JSON.parse(raw) as BoardData | SseEnvelope;
        const dataCandidate: unknown = "data" in parsed ? parsed.data : parsed;
        if (isBoardData(dataCandidate)) {
          setIsLive(true);
          applyBoard(dataCandidate);
          return;
        }
      } catch {
        // If payload is malformed we fall back to a fresh board fetch.
      }
      void fetchBoard();
    };

    events.addEventListener("snapshot", (event) => {
      applyEventData((event as MessageEvent).data);
    });

    events.onmessage = (event) => {
      applyEventData(event.data);
    };

    events.onerror = () => {
      setIsLive(false);
    };

    events.onopen = () => {
      setIsLive(true);
    };

    return () => {
      events.close();
    };
  }, [applyBoard, fetchBoard]);

  const columns = useMemo(() => {
    return WORKFLOW.map((status) => ({
      status,
      label: STAGE_META[status].label,
      icon: STAGE_META[status].icon,
      tasks: board[status] ?? [],
    }));
  }, [board]);

  return (
    <div className="flex h-[100dvh] min-h-0 flex-col overflow-hidden">
      <div className="sticky top-0 z-10 flex h-12 items-center gap-3 border-b bg-background px-3">
        <h1 className="text-sm font-semibold">Kanban Board</h1>
        <div className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
          <span className={`h-2 w-2 rounded-full ${isLive ? "bg-green-500" : "bg-amber-500"}`} />
          <span>{isLive ? "live" : "reconnecting..."}</span>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 gap-2 overflow-x-auto overflow-y-hidden px-3 py-2">
        {columns.map((column) => {
          const isInbox = column.status === "Inbox";
          const ColumnIcon = column.icon;

          return (
            <section
              key={column.status}
              className="flex min-h-[420px] w-[248px] min-w-[248px] flex-col overflow-hidden rounded-md border bg-card"
            >
              <header className="flex items-center gap-1.5 border-b px-3 py-2">
                <ColumnIcon className="h-3 w-3 text-muted-foreground" />
                <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground" aria-label={column.label}>
                  {column.label}
                </span>
                <span className="ml-auto rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-semibold">
                  {column.tasks.length}
                </span>
              </header>

              {isInbox && (
                <div className="flex gap-1.5 border-b bg-muted/30 p-2">
                  <input
                    value={inboxTitle}
                    onChange={(event) => setInboxTitle(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") {
                        const trimmed = inboxTitle.trim();
                        if (!trimmed) return;
                        void addTask(trimmed);
                        setInboxTitle("");
                      }
                    }}
                    placeholder="Add a task..."
                    className="h-8 flex-1 rounded-md border bg-background px-2 text-xs outline-none focus:ring-2 focus:ring-primary/30"
                  />
                  <button
                    type="button"
                    className="h-8 rounded-md bg-primary px-2 text-xs font-medium text-primary-foreground hover:opacity-90"
                    onClick={() => {
                      const trimmed = inboxTitle.trim();
                      if (!trimmed) return;
                      void addTask(trimmed);
                      setInboxTitle("");
                    }}
                  >
                    Add
                  </button>
                </div>
              )}

              <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto bg-muted/20 p-2">
                {column.tasks.length === 0 ? (
                  <p className="py-6 text-center text-xs italic text-muted-foreground">No tasks</p>
                ) : (
                  column.tasks.map((task) => {
                    const doneSubtasks = task.subtasks.filter((item) => item.status === "Done").length;
                    const taskStatus = (WORKFLOW.includes(task.status as TaskStatus) ? task.status : "Inbox") as TaskStatus;
                    const TaskStatusIcon = STAGE_META[taskStatus].icon;
                    return (
                      <article
                        key={task.id}
                        className={`rounded-md border bg-background p-2 shadow-sm ${task.user_input_needed ? "border-l-4 border-l-amber-400" : ""}`}
                      >
                        <p className="text-xs font-semibold leading-tight">{task.title || "(untitled)"}</p>
                        {task.description && <p className="mt-1 text-[11px] text-muted-foreground">{task.description}</p>}

                        <div className="mt-1.5 flex flex-wrap items-center gap-1 text-[10px]">
                          <span className="rounded-full border bg-muted px-1.5 py-0.5">#{task.id}</span>
                          <span className="inline-flex items-center gap-1 rounded-full border bg-muted px-1.5 py-0.5">
                            <TaskStatusIcon className="h-2.5 w-2.5" />
                            {STAGE_META[taskStatus].label}
                          </span>
                          {task.assignee && (
                            <span className="rounded-full border bg-blue-100 px-1.5 py-0.5 text-blue-700">{task.assignee}</span>
                          )}
                          {task.user_input_needed && (
                            <span className="rounded-full border bg-amber-100 px-1.5 py-0.5 text-amber-800">Input Needed</span>
                          )}
                          {task.subtasks.length > 0 && (
                            <span className="rounded-full border bg-green-100 px-1.5 py-0.5 text-green-800">
                              {doneSubtasks}/{task.subtasks.length} subtasks
                            </span>
                          )}
                          {task.attachments.length > 0 && (
                            <span className="inline-flex items-center gap-1 rounded-full border bg-violet-100 px-1.5 py-0.5 text-violet-800">
                              <Paperclip className="h-2.5 w-2.5" />
                              {task.attachments.length}
                            </span>
                          )}
                        </div>

                        <div className="mt-2 border-t pt-2">
                          <button
                            type="button"
                            className={`rounded-md border px-2 py-0.5 text-[10px] ${
                              task.user_input_needed ? "border-amber-300 bg-amber-100 text-amber-900" : "bg-background text-muted-foreground"
                            }`}
                            onClick={() => {
                              void updateTask(task.id, { user_input_needed: !task.user_input_needed });
                            }}
                          >
                            {task.user_input_needed ? "Done" : "Flag"}
                          </button>
                        </div>
                      </article>
                    );
                  })
                )}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}
