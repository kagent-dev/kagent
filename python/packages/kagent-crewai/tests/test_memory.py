import inspect
from unittest.mock import MagicMock, patch

from crewai.memory.long_term.long_term_memory import LongTermMemory
from crewai.memory.long_term.long_term_memory_item import LongTermMemoryItem
from crewai.memory.storage.ltm_sqlite_storage import LTMSQLiteStorage

from kagent.crewai._memory import KagentMemoryStorage


def test_save_signature_matches_crewai_storage_contract():
    """KagentMemoryStorage.save must accept the same keyword arguments CrewAI's
    LongTermMemory passes to its storage. CrewAI calls storage.save() with
    datetime= as a keyword (see LongTermMemory.save), and the reference
    LTMSQLiteStorage.save names that parameter 'datetime'."""
    reference_params = list(inspect.signature(LTMSQLiteStorage.save).parameters)
    kagent_params = list(inspect.signature(KagentMemoryStorage.save).parameters)
    assert kagent_params == reference_params


def test_long_term_memory_save_posts_datetime():
    """A memory-enabled CrewAI crew wires KagentMemoryStorage as its
    LongTermMemory backend. Saving must not raise and must forward the item's
    datetime to the Kagent backend."""
    storage = KagentMemoryStorage(
        thread_id="thread-1",
        user_id="user-1",
        base_url="http://kagent.test",
    )
    long_term_memory = LongTermMemory(storage)
    item = LongTermMemoryItem(
        agent="researcher",
        task="summarize the report",
        expected_output="a summary",
        datetime="2020-01-01T00:00:00",
        quality=0.9,
        metadata={"quality": 0.9},
    )

    with patch("kagent.crewai._memory.httpx.Client") as mock_client_cls:
        mock_client = MagicMock()
        mock_client_cls.return_value.__enter__.return_value = mock_client
        mock_response = MagicMock()
        mock_response.raise_for_status.return_value = None
        mock_client.post.return_value = mock_response

        long_term_memory.save(item)

        mock_client.post.assert_called_once()
        _, kwargs = mock_client.post.call_args
        memory_data = kwargs["json"]["memory_data"]
        assert memory_data["datetime"] == "2020-01-01T00:00:00"
        assert memory_data["task_description"] == "summarize the report"
        assert memory_data["score"] == 0.9
