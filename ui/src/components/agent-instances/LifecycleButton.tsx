import { useState } from "react";
import { Button, Popconfirm, Tooltip } from "antd";
import { PauseCircle, PlayCircle } from "lucide-react";
import toast from "react-hot-toast";
import { apiClient, lifecycleBlockedReason, type AgentInstance } from "@/api";
import { shortInstanceId } from "./instanceLabels";

/**
 * Suspend or resume, having asked first and having checked it is possible.
 *
 * These are the only two writes this feature offers, and they were chosen because
 * they undo each other: a reader exploring what the new runtime is doing can press
 * one, see the state change, and put it back. Create, delete, fork and share are
 * all absent for reasons recorded where the operations are declared.
 *
 * ## Why the button asks the domain whether it is possible
 *
 * The controller's workflow claims an instance from exactly one state — `READY` for
 * a suspend, `SUSPENDED` for a resume — and refuses the claim outright when another
 * operation is already in flight, answering `Aborted`. So an always-enabled button
 * would spend a round trip to be told no, and the reader would learn that only from
 * an error toast. `lifecycleBlockedReason` is that precondition, asked before
 * offering the action, and its answer becomes the tooltip on the disabled control —
 * a greyed-out button with no explanation is indistinguishable from a permissions
 * problem.
 *
 * The check is a courtesy, not a guarantee. The state on screen is as fresh as the
 * last read, so the cluster can still refuse; that refusal is reported as itself.
 */
export function LifecycleButton({
  instance,
  action,
  onDone,
  size = "small",
  showLabel = false,
}: {
  instance: AgentInstance;
  action: "suspend" | "resume";
  /** Called after a successful operation, to re-read whatever showed the instance. */
  onDone: () => void | Promise<void>;
  size?: "small" | "middle";
  /** Rows use the icon alone; the detail page has room for the word. */
  showLabel?: boolean;
}) {
  const [isRunning, setRunning] = useState(false);

  const short = shortInstanceId(instance.id);
  const blocked = lifecycleBlockedReason(instance, action);
  const verb = action === "suspend" ? "Suspend" : "Resume";
  const past = action === "suspend" ? "Suspended" : "Resumed";
  const Icon = action === "suspend" ? PauseCircle : PlayCircle;

  async function run() {
    setRunning(true);
    try {
      const call =
        action === "suspend"
          ? apiClient.agentInstances.suspend
          : apiClient.agentInstances.resume;
      await call(instance.namespace, instance.id);
      // Re-read before the toast, so the row shows the new state by the time the
      // reader is told about it — the other order congratulates them over a table
      // that still says the opposite.
      await onDone();
      toast.success(`${past} instance ${short}`);
    } catch (cause: unknown) {
      /*
       * Not a toast that disappears. A refused lifecycle operation leaves the
       * instance exactly as it was, and a reader who missed the message would
       * believe it had worked — the failure that matters most here is the
       * `Aborted` one, which arrives when somebody else's operation got there
       * first and is precisely the case where the screen looks unchanged for a
       * reason worth reading.
       */
      toast.error(
        `Could not ${action} instance ${short}: ${
          cause instanceof Error ? cause.message : String(cause)
        }`,
        { duration: Infinity },
      );
    } finally {
      setRunning(false);
    }
  }

  const label = showLabel ? verb : undefined;
  const testId = `${action}-${instance.id}`;

  if (blocked) {
    return (
      <Tooltip title={blocked}>
        {/* A disabled antd button swallows pointer events, so the tooltip needs a
            wrapper that still receives them — otherwise the explanation exists and
            is unreachable, which is the same as not having written it. */}
        <span css={{ display: "inline-block", cursor: "not-allowed" }}>
          <Button
            type="text"
            size={size}
            disabled
            icon={<Icon size={16} />}
            data-testid={testId}
            data-blocked-reason={blocked}
            aria-label={`${verb} instance ${short} — unavailable: ${blocked}`}
            css={{ pointerEvents: "none" }}
          >
            {label}
          </Button>
        </span>
      </Tooltip>
    );
  }

  return (
    <Popconfirm
      title={`${verb} instance ${short}?`}
      description={
        action === "suspend"
          ? "The instance stops running. Its state is kept, and resuming brings it back."
          : "The instance starts running again from the state it was suspended in."
      }
      okText={verb}
      okButtonProps={{ loading: isRunning, "data-testid": `confirm-${action}` }}
      cancelText="Cancel"
      onConfirm={run}
    >
      <Button
        type="text"
        size={size}
        icon={<Icon size={16} />}
        loading={isRunning}
        data-testid={testId}
        aria-label={`${verb} instance ${short}`}
      >
        {label}
      </Button>
    </Popconfirm>
  );
}
