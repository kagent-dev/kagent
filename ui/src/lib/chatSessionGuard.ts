import { TaskState, taskStateFromJSON } from "@a2a-js/sdk";
import type { Task } from "@a2a-js/sdk";
import { getSessionTasks } from "@/app/actions/sessions";
import type { ChatStatus } from "@/types";
import { mapA2AStateToStatus } from "@/lib/statusUtils";

export const RESUBSCRIBE_TASK_STATES: TaskState[] = [
  TaskState.TASK_STATE_SUBMITTED,
  TaskState.TASK_STATE_WORKING,
];
const ACTIVE_TASK_STATES: TaskState[] = [
  TaskState.TASK_STATE_SUBMITTED,
  TaskState.TASK_STATE_WORKING,
  TaskState.TASK_STATE_INPUT_REQUIRED,
];

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
    if (taskStateFromJSON(expectedTask?.status?.state) !== TaskState.TASK_STATE_INPUT_REQUIRED) {
      const inFlightTask = tasksResponse.data.findLast((task) =>
        RESUBSCRIBE_TASK_STATES.includes(taskStateFromJSON(task.status?.state)),
      );
      if (inFlightTask) {
        notify(options.messages.inFlight);
        setStatus(mapA2AStateToStatus(inFlightTask.status?.state));
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
    ACTIVE_TASK_STATES.includes(taskStateFromJSON(task.status?.state)),
  );
  if (inFlightTask) {
    if (taskStateFromJSON(inFlightTask.status?.state) === TaskState.TASK_STATE_INPUT_REQUIRED) {
      await reloadSession();
      notify(options.messages.inputRequired ?? options.messages.staleOrChanged);
    } else {
      notify(options.messages.inFlight);
      setStatus(mapA2AStateToStatus(inFlightTask.status?.state));
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
