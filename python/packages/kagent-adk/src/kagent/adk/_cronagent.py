import asyncio
import logging
import os
import sys
import time
from typing import Optional

logger = logging.getLogger(__name__)


def is_cronagent_mode() -> bool:
    """Check if running in CronAgent mode."""
    return os.getenv("KAGENT_CRONAGENT_NAME") is not None


def get_cronagent_config() -> Optional[tuple[str, str, str, str]]:
    """Get CronAgent configuration from environment variables.

    Returns:
        Tuple of (cronagent_name, initial_task, thread_policy, user_id) or None
    """
    cronagent_name = os.getenv("KAGENT_CRONAGENT_NAME")
    if not cronagent_name:
        return None

    initial_task = os.getenv("KAGENT_INITIAL_TASK", "")
    thread_policy = os.getenv("KAGENT_THREAD_POLICY", "PerRun")
    user_id = os.getenv("KAGENT_USER_ID", "system")

    return (cronagent_name, initial_task, thread_policy, user_id)


async def run_cronagent_task(kagent_app, root_agent_factory):
    """Run a CronAgent task and exit.

    This creates or retrieves a session based on thread policy,
    executes the initial task, and exits with status 0 on success or 1 on failure.
    """
    config = get_cronagent_config()
    if not config:
        logger.error("CronAgent mode enabled but configuration not found")
        sys.exit(1)

    cronagent_name, initial_task, thread_policy, user_id = config

    logger.info(f"Running in CronAgent mode: {cronagent_name}")
    logger.info(f"Thread policy: {thread_policy}")
    logger.info(f"User ID: {user_id}")
    logger.info(f"Task: {initial_task[:100]}...")

    # Determine agent_id based on thread policy
    if thread_policy == "Persistent":
        # Use cronagent name as agent_id to share session across runs
        agent_id = cronagent_name
        logger.info(f"Using persistent session for: {agent_id}")
    else:  # PerRun
        # Create unique agent_id for this run
        agent_id = f"cronagent-{cronagent_name}-{int(time.time())}"
        logger.info(f"Creating new session for run: {agent_id}")

    # Get or create session using the app's session service
    try:
        session_service = kagent_app.session_service

        # Try to get existing session
        session = await session_service.get_session(agent_id, user_id)

        if not session:
            logger.info(f"Creating new session for agent_id: {agent_id}")
            from a2a.types import Message, SessionInfo, MessageRole

            # Create new session
            session_info = SessionInfo(
                session_id=None,  # Will be auto-generated
                agent_id=agent_id,
                user_id=user_id,
                messages=[],
            )
            session = await session_service.create_session(session_info)
            logger.info(f"Created session: {session.session_id}")
        else:
            logger.info(f"Using existing session: {session.session_id}")

    except Exception as e:
        logger.error(f"Failed to get/create session: {e}", exc_info=True)
        sys.exit(1)

    # Execute the task
    try:
        logger.info(f"Executing CronAgent task...")

        from a2a.types import Message, MessageRole, Task

        # Create task message
        task_msg = Message(
            role=MessageRole.USER,
            content=initial_task,
        )

        # Create task
        task = Task(message=task_msg)

        # Execute using the agent executor
        root_agent = root_agent_factory()

        # Import agent executor
        from ._agent_executor import AgentExecutor

        executor = AgentExecutor(
            root_agent=root_agent,
            session_service=session_service,
            stream=False,  # Don't stream for cron tasks
        )

        # Execute and collect result
        result_messages = []
        async for event in executor.execute(
            session_id=session.session_id,
            user_id=user_id,
            task=task,
        ):
            # Collect agent responses
            if hasattr(event, 'message'):
                result_messages.append(event.message)

        logger.info(f"Task completed successfully")
        if result_messages:
            logger.info(f"Generated {len(result_messages)} response message(s)")

    except Exception as e:
        logger.error(f"Task execution failed: {e}", exc_info=True)
        sys.exit(1)

    # Exit successfully
    logger.info("CronAgent task complete, exiting")
    sys.exit(0)
