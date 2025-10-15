"""Integration tests for workflow context propagation with shared sessions."""

import asyncio
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest
from google.adk.events import Event
from google.adk.sessions import Session
from google.genai.types import Content, Part

# Import the types we'll need once implemented
# from kagent.adk.agents.sequential import KAgentSequentialAgent


@pytest.mark.asyncio
@pytest.mark.integration
class TestWorkflowContextPropagation:
    """Integration tests for context propagation in sequential workflows."""

    async def test_sub_agent_sees_previous_events(self):
        """Test that sub-agent-2 can see events from sub-agent-1 in shared session.

        Simulates a sequential workflow where:
        1. Sub-agent-1 executes and generates events
        2. Events are appended to shared session
        3. Sub-agent-2 executes and fetches session
        4. Sub-agent-2's session contains sub-agent-1's events

        This is T013 from tasks.md.
        """
        # TODO: This test requires KAgentSequentialAgent implementation
        # and will be completed after T014-T019 are done

        # For now, mark as expected to fail
        pytest.skip("Requires KAgentSequentialAgent implementation (T014-T019)")

        # Future implementation outline:
        # 1. Create mock session service
        # 2. Create mock sub-agents that:
        #    - Sub-agent-1: Generates event with tool call data
        #    - Sub-agent-2: Accesses session.events to see sub-agent-1's data
        # 3. Execute KAgentSequentialAgent with both sub-agents
        # 4. Assert sub-agent-2 sees sub-agent-1's events in its context


@pytest.mark.asyncio
@pytest.mark.integration
class TestSessionPersistence:
    """Integration tests for session persistence and retrieval (User Story 2)."""

    async def test_session_query_returns_all_events(self):
        """Test querying session after workflow returns all sub-agent events.

        This is T025 from tasks.md (User Story 2).

        Simulates a sequential workflow where:
        1. Create a shared session
        2. Multiple sub-agents execute and append events
        3. Query the session
        4. Verify all events from all sub-agents are returned
        """
        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        # Shared session ID for the workflow
        session_id = "test-workflow-session-123"
        user_id = "test-user"
        app_name = "test-sequential-workflow"

        # Mock session creation response
        mock_client.post.return_value = MagicMock(
            status_code=200,
            json=lambda: {"data": {"id": session_id, "user_id": user_id, "agent_id": "test_sequential_workflow"}},
        )

        # Create session
        session = await session_service.create_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Create mock events from different sub-agents
        event1 = Event(author="sub-agent-1", content=Content(role="model", parts=[Part(text="Sub-agent 1 executed")]))

        event2 = Event(author="sub-agent-2", content=Content(role="model", parts=[Part(text="Sub-agent 2 executed")]))

        event3 = Event(author="sub-agent-3", content=Content(role="model", parts=[Part(text="Sub-agent 3 executed")]))

        # Mock event append responses
        mock_client.post.return_value = MagicMock(status_code=200, json=lambda: {"data": {"id": "event-id"}})

        # Append events (simulating sub-agent execution)
        await session_service.append_event(session, event1)
        await session_service.append_event(session, event2)
        await session_service.append_event(session, event3)

        # Mock session query response with all events
        mock_client.get.return_value = MagicMock(
            status_code=200,
            json=lambda: {
                "data": {
                    "session": {"id": session_id, "user_id": user_id, "agent_id": "test_sequential_workflow"},
                    "events": [
                        {"id": event1.id, "data": event1.model_dump_json(), "created_at": "2025-10-14T12:00:00Z"},
                        {"id": event2.id, "data": event2.model_dump_json(), "created_at": "2025-10-14T12:01:00Z"},
                        {"id": event3.id, "data": event3.model_dump_json(), "created_at": "2025-10-14T12:02:00Z"},
                    ],
                }
            },
        )

        # Query session to get all events
        fetched_session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Verify all events are returned
        assert fetched_session is not None
        assert len(fetched_session.events) == 3
        assert fetched_session.events[0].id == event1.id
        assert fetched_session.events[1].id == event2.id
        assert fetched_session.events[2].id == event3.id

        # Verify events are from different sub-agents
        authors = {event.author for event in fetched_session.events}
        assert authors == {"sub-agent-1", "sub-agent-2", "sub-agent-3"}

    async def test_event_chronological_ordering(self):
        """Test that events are ordered chronologically by created_at timestamp.

        This is T026 from tasks.md (User Story 2).

        Verifies:
        1. Events from session query are ordered by timestamp
        2. Earlier events come first
        3. Timestamp ordering is consistent across sub-agents
        """
        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        session_id = "test-ordering-session"
        user_id = "test-user"
        app_name = "test-app"

        # Create events with specific timestamps
        event1 = Event(author="sub-agent-1", content=Content(role="model", parts=[Part(text="First event")]))

        event2 = Event(author="sub-agent-2", content=Content(role="model", parts=[Part(text="Second event")]))

        event3 = Event(author="sub-agent-3", content=Content(role="model", parts=[Part(text="Third event")]))

        # Mock session query response with events in chronological order
        mock_client.get.return_value = MagicMock(
            status_code=200,
            json=lambda: {
                "data": {
                    "session": {"id": session_id, "user_id": user_id},
                    "events": [
                        {"id": event1.id, "data": event1.model_dump_json(), "created_at": "2025-10-14T12:00:00Z"},
                        {"id": event2.id, "data": event2.model_dump_json(), "created_at": "2025-10-14T12:01:00Z"},
                        {"id": event3.id, "data": event3.model_dump_json(), "created_at": "2025-10-14T12:02:00Z"},
                    ],
                }
            },
        )

        # Fetch session
        session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Verify events are in chronological order
        assert session is not None
        assert len(session.events) == 3

        # Verify timestamps are in ascending order (timestamps are Unix epoch floats)
        timestamps = [event.timestamp for event in session.events]
        assert timestamps == sorted(timestamps), "Events should be ordered chronologically"

        # Verify specific ordering by event IDs
        assert session.events[0].id == event1.id
        assert session.events[1].id == event2.id
        assert session.events[2].id == event3.id

    async def test_event_author_attribution(self):
        """Test that each event's author field identifies the sub-agent.

        This is T027 from tasks.md (User Story 2).

        Verifies:
        1. Each event has an author field
        2. Author field correctly identifies the sub-agent that generated the event
        3. Different sub-agents have different author values
        """
        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        session_id = "test-author-session"
        user_id = "test-user"
        app_name = "test-app"

        # Create events with specific authors
        event1 = Event(author="pre-check-agent", content=Content(role="model", parts=[Part(text="Pre-check passed")]))

        event2 = Event(author="deploy-agent", content=Content(role="model", parts=[Part(text="Deployment successful")]))

        event3 = Event(author="validate-agent", content=Content(role="model", parts=[Part(text="Validation complete")]))

        # Mock session query response
        mock_client.get.return_value = MagicMock(
            status_code=200,
            json=lambda: {
                "data": {
                    "session": {"id": session_id, "user_id": user_id},
                    "events": [
                        {"id": event1.id, "data": event1.model_dump_json(), "created_at": "2025-10-14T12:00:00Z"},
                        {"id": event2.id, "data": event2.model_dump_json(), "created_at": "2025-10-14T12:01:00Z"},
                        {"id": event3.id, "data": event3.model_dump_json(), "created_at": "2025-10-14T12:02:00Z"},
                    ],
                }
            },
        )

        # Fetch session
        session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Verify each event has correct author attribution
        assert session is not None
        assert len(session.events) == 3

        # Verify all events have author field
        for event in session.events:
            assert hasattr(event, "author"), "Event should have author field"
            assert event.author is not None, "Author should not be None"
            assert len(event.author) > 0, "Author should not be empty"

        # Verify correct author for each event
        assert session.events[0].author == "pre-check-agent"
        assert session.events[1].author == "deploy-agent"
        assert session.events[2].author == "validate-agent"

        # Verify all authors are different (each sub-agent identified)
        authors = [event.author for event in session.events]
        assert len(set(authors)) == len(authors), "Each sub-agent should have unique author"

    async def test_session_query_with_empty_events(self):
        """Test querying a session with no events returns empty list.

        This is T030 from tasks.md (User Story 2).

        Verifies:
        1. Newly created session has empty events list
        2. Session query succeeds even with no events
        3. Response structure is consistent
        """
        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        session_id = "test-empty-session"
        user_id = "test-user"
        app_name = "test-app"

        # Mock session creation
        mock_client.post.return_value = MagicMock(
            status_code=200, json=lambda: {"data": {"id": session_id, "user_id": user_id}}
        )

        # Create new session
        await session_service.create_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Mock session query response with empty events
        mock_client.get.return_value = MagicMock(
            status_code=200, json=lambda: {"data": {"session": {"id": session_id, "user_id": user_id}, "events": []}}
        )

        # Query session
        fetched_session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Verify empty events list
        assert fetched_session is not None
        assert len(fetched_session.events) == 0
        assert fetched_session.id == session_id
        assert fetched_session.user_id == user_id

    async def test_session_query_with_many_events(self):
        """Test querying a session with 100+ events.

        This is T031 from tasks.md (User Story 2).

        Verifies:
        1. Large sessions can be fetched successfully
        2. All events are returned when limit=-1
        3. Event order is maintained with large datasets
        """
        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        session_id = "test-large-session"
        user_id = "test-user"
        app_name = "test-app"

        # Generate 150 events
        num_events = 150
        events_list = []
        mock_events = []
        for i in range(num_events):
            event = Event(
                author=f"sub-agent-{i % 3}",  # Rotate through 3 sub-agents
                content=Content(role="model", parts=[Part(text=f"Event {i}")]),
            )
            events_list.append(event)
            mock_events.append(
                {
                    "id": event.id,
                    "data": event.model_dump_json(),
                    "created_at": f"2025-10-14T12:{i // 60:02d}:{i % 60:02d}Z",
                }
            )

        # Mock session query response with many events
        mock_client.get.return_value = MagicMock(
            status_code=200,
            json=lambda: {"data": {"session": {"id": session_id, "user_id": user_id}, "events": mock_events}},
        )

        # Query session
        fetched_session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Verify all events returned
        assert fetched_session is not None
        assert len(fetched_session.events) == num_events

        # Verify event order maintained
        for i in range(num_events):
            assert fetched_session.events[i].id == events_list[i].id

        # Verify events are from multiple sub-agents
        authors = {event.author for event in fetched_session.events}
        assert len(authors) == 3  # Should have 3 different sub-agents

    async def test_failed_subagent_error_events_visible(self):
        """Test that error events from failed sub-agents are visible in session.

        This is T032 from tasks.md (User Story 2).

        Verifies:
        1. Error events are persisted to session
        2. Error events have proper author attribution
        3. Error events can be queried alongside successful events
        """
        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        session_id = "test-error-session"
        user_id = "test-user"
        app_name = "test-app"

        # Create events including an error event
        event1 = Event(author="sub-agent-1", content=Content(role="model", parts=[Part(text="Sub-agent 1 successful")]))

        # Error event from failed sub-agent
        error_event = Event(
            author="sub-agent-2",
            content=Content(role="model", parts=[Part(text="Error: Failed to execute tool - connection timeout")]),
        )

        event3 = Event(
            author="sub-agent-3", content=Content(role="model", parts=[Part(text="Sub-agent 3 handling error")])
        )

        # Mock session query response with error event
        mock_client.get.return_value = MagicMock(
            status_code=200,
            json=lambda: {
                "data": {
                    "session": {"id": session_id, "user_id": user_id},
                    "events": [
                        {"id": event1.id, "data": event1.model_dump_json(), "created_at": "2025-10-14T12:00:00Z"},
                        {
                            "id": error_event.id,
                            "data": error_event.model_dump_json(),
                            "created_at": "2025-10-14T12:01:00Z",
                        },
                        {"id": event3.id, "data": event3.model_dump_json(), "created_at": "2025-10-14T12:02:00Z"},
                    ],
                }
            },
        )

        # Query session
        fetched_session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        # Verify error event is present
        assert fetched_session is not None
        assert len(fetched_session.events) == 3

        # Find the error event
        error_events = [e for e in fetched_session.events if "Error:" in str(e.content)]
        assert len(error_events) == 1

        # Verify error event has correct author
        assert error_events[0].author == "sub-agent-2"

        # Verify error event is in correct chronological position
        event_ids = [e.id for e in fetched_session.events]
        assert event_ids.index(error_event.id) == 1  # Second event


@pytest.mark.asyncio
@pytest.mark.integration
class TestMemoryManagement:
    """Integration tests for memory management with shared sessions (User Story 3)."""

    async def test_concurrent_workflows_memory(self):
        """Test memory usage with concurrent sequential workflows.

        This is T036 from tasks.md (User Story 3).

        Verifies:
        1. Memory usage stays within acceptable bounds with concurrent workflows
        2. Memory returns to baseline after workflows complete
        3. No memory leaks with shared sessions
        """
        import sys
        from pathlib import Path

        # Add fixtures to path
        fixtures_path = Path(__file__).parent.parent / "fixtures"
        sys.path.insert(0, str(fixtures_path))

        from memory_utils import MemoryProfiler

        from kagent.adk._session_service import KAgentSessionService

        profiler = MemoryProfiler()
        profiler.start_profiling()

        # Capture baseline
        baseline_snapshot = profiler.capture_snapshot("baseline")

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        # Mock responses for session operations
        def mock_post_response(*args, **kwargs):
            """Mock POST responses for session/event creation."""
            return MagicMock(status_code=200, json=lambda: {"data": {"id": "test-id", "user_id": "test-user"}})

        def mock_get_response(*args, **kwargs):
            """Mock GET responses for session fetch."""
            return MagicMock(
                status_code=200,
                json=lambda: {
                    "data": {
                        "session": {"id": "test-session", "user_id": "test-user"},
                        "events": [
                            {
                                "id": f"event-{i}",
                                "data": Event(
                                    author="test-agent", content=Content(role="model", parts=[Part(text=f"Event {i}")])
                                ).model_dump_json(),
                                "created_at": "2025-10-14T12:00:00Z",
                            }
                            for i in range(10)
                        ],
                    }
                },
            )

        mock_client.post = AsyncMock(side_effect=mock_post_response)
        mock_client.get = AsyncMock(side_effect=mock_get_response)

        # Run 10 concurrent workflows (scaled down from 100 for test speed)
        num_workflows = 10

        async def simulate_workflow(workflow_id: int, service: KAgentSessionService):
            """Simulate a sequential workflow execution."""
            session_id = f"concurrent-workflow-{workflow_id}"
            user_id = "test-user"
            app_name = "test-workflow"

            # Create session
            session = await service.create_session(app_name=app_name, user_id=user_id, session_id=session_id)

            # Simulate 3 sub-agents appending events
            for sub_agent_idx in range(3):
                event = Event(
                    author=f"sub-agent-{sub_agent_idx}",
                    content=Content(role="model", parts=[Part(text=f"Workflow {workflow_id}, sub-agent {sub_agent_idx}")]),
                )
                await service.append_event(session, event)

            # Fetch session (simulating context propagation)
            await service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

            return session_id

        # Execute workflows concurrently
        workflow_tasks = [simulate_workflow(i, session_service) for i in range(num_workflows)]
        completed_sessions = await asyncio.gather(*workflow_tasks)

        # Capture peak memory after workflows
        peak_snapshot = profiler.capture_snapshot("after_workflows")

        # Verify all workflows completed
        assert len(completed_sessions) == num_workflows

        # Clear references
        del completed_sessions
        del workflow_tasks
        del session_service
        del mock_client

        # Force garbage collection and capture final memory
        import gc

        gc.collect()
        gc.collect()
        gc.collect()

        final_snapshot = profiler.capture_snapshot("after_gc")

        # Stop profiling
        profiler.stop_profiling()

        # Log memory summary for debugging (accessible via pytest -s)
        import sys

        sys.stderr.write("\n" + profiler.get_memory_summary() + "\n")

        # Assert: Memory returns to baseline within 10% threshold (T040)
        # This verifies no memory leaks with shared sessions
        memory_delta_mb = final_snapshot["rss_delta_mb"]
        memory_delta_percent = (memory_delta_mb / baseline_snapshot["rss_mb"]) * 100

        # Allow up to 10% memory increase (should be minimal with proper cleanup)
        assert (
            memory_delta_percent <= 10.0
        ), f"Memory not released to baseline: {memory_delta_mb:.2f} MB increase ({memory_delta_percent:.1f}%), threshold is 10%"

        # Additional validation: Peak memory should be reasonable
        # With 10 workflows, peak should not exceed baseline + 50MB
        peak_delta_mb = peak_snapshot["rss_delta_mb"]
        assert peak_delta_mb < 50.0, f"Peak memory too high: {peak_delta_mb:.2f} MB increase from baseline"

    async def test_large_session_fetch_performance(self):
        """Test fetching sessions with 1000+ events completes in < 500ms.

        This is T037 from tasks.md (User Story 3).

        Verifies:
        1. Large sessions can be fetched efficiently
        2. Fetch time is under 500ms for 1000 events
        3. No performance degradation with large event lists
        """
        import time

        from kagent.adk._session_service import KAgentSessionService

        # Create mock HTTP client
        mock_client = AsyncMock(spec=httpx.AsyncClient)
        session_service = KAgentSessionService(client=mock_client)

        session_id = "large-session-1000"
        user_id = "test-user"
        app_name = "test-app"

        # Generate 1000 events for the mock response
        num_events = 1000
        mock_events = []
        for i in range(num_events):
            event = Event(
                author=f"sub-agent-{i % 5}",  # Rotate through 5 sub-agents
                content=Content(role="model", parts=[Part(text=f"Event {i} with some realistic content and data")]),
            )
            mock_events.append(
                {
                    "id": event.id,
                    "data": event.model_dump_json(),
                    "created_at": f"2025-10-14T12:{i // 60:02d}:{i % 60:02d}Z",
                }
            )

        # Mock session query response with 1000 events
        mock_client.get.return_value = MagicMock(
            status_code=200,
            json=lambda: {"data": {"session": {"id": session_id, "user_id": user_id}, "events": mock_events}},
        )

        # Measure fetch time
        start_time = time.perf_counter()

        fetched_session = await session_service.get_session(app_name=app_name, user_id=user_id, session_id=session_id)

        end_time = time.perf_counter()
        fetch_time_ms = (end_time - start_time) * 1000

        # Verify session fetched successfully
        assert fetched_session is not None
        assert len(fetched_session.events) == num_events

        # Verify fetch time is under 500ms
        # Note: This is a mocked test, so actual time depends on JSON parsing
        # In production, database query time is the main factor
        import sys

        sys.stderr.write(f"\nFetch time for {num_events} events: {fetch_time_ms:.2f} ms\n")

        # Allow some overhead for test environment, but should be well under 500ms
        assert fetch_time_ms < 500.0, f"Session fetch too slow: {fetch_time_ms:.2f} ms (threshold: 500 ms)"

        # Verify event data integrity
        assert fetched_session.events[0].author == "sub-agent-0"
        assert fetched_session.events[-1].author == f"sub-agent-{(num_events - 1) % 5}"

        # Verify all events have unique IDs
        event_ids = [e.id for e in fetched_session.events]
        assert len(event_ids) == len(set(event_ids)), "All events should have unique IDs"
