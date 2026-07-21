import type { Task, TaskState } from "@a2a-js/sdk";
import { getSessionTasks } from "@/app/actions/sessions";
import type { ChatStatus } from "@/types";
import { mapA2AStateToStatus } from "@/lib/statusUtils";

export const RESUBSCRIBE_TASK_STATES: TaskState[] = ["submitted", "working"];
const ACTIVE_TASK_STATES: TaskState[] = ["submitted", "working", "input-required"];

export const countServerMessages = (tasks: Task[]): number =>
  tasks.reduce((sum, task) => sum + (task.history?.length ?? 0), 0);

export type SessionGuardOptions = {
  expectedTaskId?: string;
  messages: {
    inFlight: string;
    inputRequired?: string;
    staleOrChanged: string;
  };
};

export async function checkAndSyncChatSession({
  sessionId,
  syncedServerMessageCount,
  options,
  reloadSession,
  resubscribeTask,
  setStatus,
  notify,
}: {
  sessionId: string;
  syncedServerMessageCount: number;
  options: SessionGuardOptions;
  reloadSession: () => Promise<void>;
  resubscribeTask: (taskId: string) => Promise<void>;
  setStatus: (status: ChatStatus) => void;
  notify: (message: string) => void;
}): Promise<"proceed" | "blocked"> {
  let tasksResponse: Awaited<ReturnType<typeof getSessionTasks>>;
  try {
    tasksResponse = await getSessionTasks(sessionId);
  } catch {
    return "proceed";
  }
  if (!tasksResponse.data) return "proceed";

  if (options.expectedTaskId) {
    const expectedTask = tasksResponse.data.findLast((task) => task.id === options.expectedTaskId);
    if ((expectedTask?.status?.state as TaskState | undefined) !== "input-required") {
      const inFlightTask = tasksResponse.data.findLast((task) =>
        RESUBSCRIBE_TASK_STATES.includes(task.status?.state as TaskState),
      );
      if (inFlightTask) {
        notify(options.messages.inFlight);
        setStatus(mapA2AStateToStatus(inFlightTask.status?.state as TaskState));
        await resubscribeTask(inFlightTask.id);
      } else {
        await reloadSession();
        notify(options.messages.staleOrChanged);
      }
      return "blocked";
    }
    return "proceed";
  }

  const inFlightTask = tasksResponse.data.findLast((task) =>
    ACTIVE_TASK_STATES.includes(task.status?.state as TaskState),
  );
  if (inFlightTask) {
    if ((inFlightTask.status?.state as TaskState) === "input-required") {
      await reloadSession();
      notify(options.messages.inputRequired ?? options.messages.staleOrChanged);
    } else {
      notify(options.messages.inFlight);
      setStatus(mapA2AStateToStatus(inFlightTask.status?.state as TaskState));
      await resubscribeTask(inFlightTask.id);
    }
    return "blocked";
  }

  if (countServerMessages(tasksResponse.data) > syncedServerMessageCount) {
    await reloadSession();
    notify(options.messages.staleOrChanged);
    return "blocked";
  }

  return "proceed";
}
