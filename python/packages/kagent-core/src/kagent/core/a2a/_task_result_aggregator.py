from a2a.server.events import Event
from a2a.types import Message, TaskState, TaskStatusUpdateEvent


class TaskResultAggregator:
    """Aggregates the task status updates and provides the final task state."""

    def __init__(self):
        self._task_state = TaskState.working
        self._task_status_message = None

    def process_event(self, event: Event):
        """Process an event from the agent run and detect signals about the task status.
        Priority of task state:
        - failed
        - auth_required
        - input_required
        - working
        """
        if isinstance(event, TaskStatusUpdateEvent):
            if event.status.state == TaskState.failed:
                self._task_state = TaskState.failed
                self._task_status_message = event.status.message
            elif event.status.state == TaskState.auth_required and self._task_state != TaskState.failed:
                self._task_state = TaskState.auth_required
                self._task_status_message = event.status.message
            elif event.status.state == TaskState.input_required and self._task_state not in (
                TaskState.failed,
                TaskState.auth_required,
            ):
                self._task_state = TaskState.input_required
                self._task_status_message = event.status.message
            # final state is already recorded and make sure the intermediate state is
            # always working because other state may terminate the event aggregation
            # in a2a request handler
            elif self._task_state == TaskState.working:
                self._task_status_message = event.status.message
            event.status.state = TaskState.working

    @property
    def task_state(self) -> TaskState:
        return self._task_state

    @property
    def task_status_message(self) -> Message | None:
        return self._task_status_message
