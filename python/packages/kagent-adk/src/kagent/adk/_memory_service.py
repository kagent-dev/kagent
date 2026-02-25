"""Kagent Memory Service implementation conforming to ADK BaseMemoryService interface."""

import json
import logging
from typing import Any, Dict, List, Optional, Union

import httpx
import numpy as np
from google.adk.memory import BaseMemoryService
from google.adk.memory.base_memory_service import SearchMemoryResponse
from google.adk.memory.memory_entry import MemoryEntry
from google.adk.models import BaseLlm
from google.adk.sessions import Session
from google.genai import types
from litellm import aembedding

from kagent.adk.types import EmbeddingConfig

logger = logging.getLogger(__name__)


class KagentMemoryService(BaseMemoryService):
    """Memory service that stores and retrieves memories via Kagent backend.

    This service:
    1. Extracts text content from Session events
    2. Generates embeddings using LiteLLM
    3. Stores/searches via Kagent API backed by pgvector
    """

    def __init__(
        self,
        agent_name: str,
        http_client: httpx.AsyncClient,
        embedding_config: Optional[EmbeddingConfig] = None,
        ttl_days: int = 0,
    ):
        """Initialize KagentMemoryService.

        Args:
            agent_name: Name of the agent (used as namespace in storage)
            http_client: Async HTTP client configured with base_url for Kagent API
            embedding_config: Configuration for embedding model (EmbeddingConfig only).
            ttl_days: TTL for memory entries in days. 0 means use the server default.
        """
        self.agent_name = agent_name
        self.client = http_client
        self.embedding_config = embedding_config
        self.ttl_days = ttl_days

    async def add_session_to_memory(self, session: Session, model: Optional[Any] = None) -> None:
        """Add a session's content to long-term memory.

        Extracts text from session events, summarizes it using LLM, generates
        an embedding, and stores it in the Kagent backend with TTL support.

        Args:
            session: The session to add to memory
            model: Optional ADK model object (e.g., LiteLlm, OpenAI) to use for summarization.
        """
        # Extract content from session events
        raw_content = self._extract_session_content(session)
        if not raw_content:
            logger.debug("No content to add to memory from session %s", session.id)
            return

        logger.debug("Adding session %s to memory for user %s", session.id, session.user_id)

        # Summarize content before embedding
        # Returns a list of strings (individual facts/memories)
        contents = await self._summarize_session_content_async(raw_content, model=model)

        # Filter out empty content items
        valid_contents = [c for c in contents if c]
        if not valid_contents:
            return

        logger.debug("Generating embeddings for %d content items", len(valid_contents))

        # Batch generate embeddings
        vectors = await self._generate_embedding_async(valid_contents)
        if not vectors:
            logger.warning("Failed to generate embeddings for session %s", session.id)
            return

        if not isinstance(vectors[0], (list, np.ndarray)):
            # vectors is a flat list of floats (single vector); wrap it
            vectors = [vectors]

        # Prepare batch items
        batch_items = []

        # Iterate over synced content and vectors
        for content_item, vector in zip(valid_contents, vectors, strict=True):
            if not vector:
                continue

            item: Dict[str, Any] = {
                "agent_name": self.agent_name,
                "user_id": session.user_id,
                "content": content_item,
                "vector": vector,
            }
            if self.ttl_days > 0:
                item["ttl_days"] = self.ttl_days
            batch_items.append(item)

        if not batch_items:
            return

        try:
            response = await self.client.post("/api/memories/sessions/batch", json={"items": batch_items})
            if response.status_code >= 400:
                logger.error("Response body: %s", response.text)
            response.raise_for_status()
            logger.info("Successfully saved %d memory items via batch API", len(batch_items))
        except Exception as e:
            logger.error("Failed to save session %s memory items: %s", session.id, e)

    async def add_memory(
        self,
        *,
        app_name: str,
        user_id: str,
        content: str,
        metadata: Optional[Dict[str, Any]] = None,
    ) -> None:
        """Add a specific text content to memory.

        Args:
            app_name: The application name
            user_id: The user ID
            content: The text content to save
            metadata: Optional additional metadata
        """
        if not content:
            return

        logger.debug("Adding specific content to memory for user %s", user_id)

        # Generate embedding
        vector = await self._generate_embedding_async(content)
        if not vector:
            logger.warning("Failed to generate embedding for memory content")
            return

        # Send to Kagent API
        payload: Dict[str, Any] = {
            "agent_name": self.agent_name,
            "user_id": user_id,
            "content": content,
            "vector": vector,
        }
        if self.ttl_days > 0:
            payload["ttl_days"] = self.ttl_days

        try:
            response = await self.client.post("/api/memories/sessions", json=payload)
            if response.status_code >= 400:
                logger.error("Response body: %s", response.text)
            response.raise_for_status()
            memory_id = response.json().get("id")
            logger.info("Successfully saved memory item (id=%s)", memory_id)
        except Exception as e:
            logger.error("Failed to save memory: %s", e)

    async def search_memory(
        self,
        *,
        app_name: str,
        user_id: str,
        query: str,
    ) -> SearchMemoryResponse:
        """Search memory for relevant content.

        Args:
            app_name: The application name (used for filtering)
            user_id: The user ID to search within
            query: The search query text

        Returns:
            SearchMemoryResponse containing matching MemoryEntry objects
        """
        # Generate embedding for the query
        vector = await self._generate_embedding_async(query)
        if not vector:
            logger.warning("Failed to generate embedding for search query")
            return SearchMemoryResponse(memories=[])

        payload = {
            "agent_name": self.agent_name,
            "user_id": user_id,
            "vector": vector,
            "limit": 5,
            "min_score": 0.3,
        }

        try:
            response = await self.client.post("/api/memories/search", json=payload)
            if response.status_code >= 400:
                logger.error("Response body: %s", response.text)
            response.raise_for_status()
            results = response.json()

            memories = []
            for item in results:
                content = types.Content(
                    role="user",
                    parts=[types.Part(text=item.get("content", ""))],
                )
                memory_entry = MemoryEntry(id=item.get("id"), content=content)
                memories.append(memory_entry)

            if len(memories) == 0:
                logger.warning("No memories found for query: %s", query)
                return SearchMemoryResponse(memories=[])

            logger.info("Successfully retrieved memories for query: %s", query)
            return SearchMemoryResponse(memories=memories)
        except Exception as e:
            logger.error("Failed to search memory: %s", e)
            return SearchMemoryResponse(memories=[])

    def _extract_session_content(self, session: Session) -> str:
        """Extract text content from session events.

        Combines all user and agent messages into a single searchable text.
        Filters out tool calls to reduce noise, but keeps tool outputs.

        Args:
            session: The session to extract content from

        Returns:
            Combined text content from the session
        """
        parts = []

        for event in session.events or []:
            if event.content and event.content.parts:
                for part in event.content.parts:
                    # Skip tool calls and executable code requests
                    if hasattr(part, "function_call") and part.function_call:
                        continue
                    if hasattr(part, "executable_code") and part.executable_code:
                        continue

                    role = event.author or "unknown"
                    text_content = None

                    # Prefer existing text if available
                    if hasattr(part, "text") and part.text:
                        text_content = part.text

                    # Fallback: Extract content from tool responses if text is missing
                    elif hasattr(part, "function_response") and part.function_response:
                        try:
                            # Attempt to serialize the response payload
                            response_data = getattr(part.function_response, "response", None)
                            if response_data:
                                text_content = json.dumps(response_data, default=str)
                        except Exception:
                            logger.warning("Failed to serialize function_response payload", exc_info=True)

                    elif hasattr(part, "code_execution_result") and part.code_execution_result:
                        try:
                            # Typically has 'output' field
                            output = getattr(part.code_execution_result, "output", None)
                            if output:
                                text_content = output
                        except Exception:
                            logger.warning("Failed to extract code_execution_result output", exc_info=True)

                    if text_content:
                        parts.append(f"{role}: {text_content}")

        return "\n".join(parts)

    def _normalize_l2(self, x):
        x = np.array(x)
        if x.ndim == 1:
            norm = np.linalg.norm(x)
            if norm == 0:
                return x
            return x / norm
        else:
            norm = np.linalg.norm(x, 2, axis=1, keepdims=True)
            return np.where(norm == 0, x, x / norm)

    async def _generate_embedding_async(
        self, input_data: Union[str, List[str]]
    ) -> Union[List[float], List[List[float]]]:
        """Generate embedding vector(s) using LiteLLM.

        Args:
            input_data: Single string or list of strings to embed.

        Returns:
            Single vector (List[float]) if input is string,
            or List of vectors (List[List[float]]) if input is list.
            Returns empty list on failure.
        """
        if not self.embedding_config:
            logger.warning("No embedding configuration found")
            return []

        model_name = self.embedding_config.model
        provider = self.embedding_config.provider

        if not model_name:
            logger.warning("No embedding model specified in config")
            return []

        # Build LiteLLM model identifier
        litellm_model = model_name
        if provider and provider != "openai" and "/" not in model_name:
            if provider == "azure_openai":
                litellm_model = f"azure/{model_name}"
            elif provider == "ollama":
                litellm_model = f"ollama/{model_name}"
            elif provider == "vertex_ai":
                litellm_model = f"vertex_ai/{model_name}"

        try:
            is_batch = isinstance(input_data, list)
            texts = input_data if is_batch else [input_data]

            # Most Matryoshka Representation Learning embedding models produce embeddings that still have meaning when truncated to specific sizes
            # https://huggingface.co/blog/matryoshka
            # We must ensure that embeddings have proper dimensions for compatibility with vector storage backend
            api_base = self.embedding_config.base_url or None
            response = await aembedding(model=litellm_model, input=texts, dimensions=768, api_base=api_base)

            embeddings = []
            for item in response.data:
                embedding = item["embedding"]

                # LiteLLM does not truncate embeddings by default if the model doesn't support it
                # However, truncating embeddings is still valid (for most models, see OpenAI's docs and this research https://arxiv.org/html/2508.17744v1)
                if len(embedding) > 768:
                    embedding = embedding[:768]
                    # if we change dimension manually, we need to re-normalize the embeddings
                    embedding = self._normalize_l2(embedding)

                embeddings.append(embedding)

            if is_batch:
                return embeddings
            return embeddings[0] if embeddings else []

        except Exception as e:
            logger.error("Error generating embedding with model %s: %s", litellm_model, e)
            return []

    async def _summarize_session_content_async(
        self,
        content: str,
        model: Optional[BaseLlm] = None,
    ) -> List[str]:
        """Summarize session content using an LLM before embedding.

        This extracts key facts, decisions, and user preferences from the conversation
        to create a more semantic-search-friendly representation.

        Args:
            content: The raw session content to summarize
            model: Optional ADK model object (e.g., LiteLlm, OpenAI) to use.
                   If not provided, summarization is skipped.

        Returns:
            List of summarized content strings, or list containing original content if summarization fails/skipped
        """
        if model is None:
            logger.debug("No model provided for summarization, using original content")
            return [content]

        # NOTE: In the future, we may allow configuring a separate, potentially cheaper
        # model specifically for summarization tasks to optimize costs.
        prompt = """Extract and summarize the key information from this conversation that would be useful for the agent to remember in future interactions.

Focus on:
- User preferences, decisions, and explicit requests
- Important facts mentioned (names, dates, project names, etc.)
- Action items or commitments made
- Contextual information that provides background
- Lessons learned from the conversation

You MUST output a JSON list of strings, where each string is a distinct fact or memory.
Example: ["User prefers dark mode", "Meeting scheduled for Friday", "Always use the save_memory tool to store memory"]

Do not include any preamble or markdown formatting like ```json.
Output ONLY the JSON list.

Conversation:
{content}

Summary (JSON List):"""

        try:
            from google.adk.models.llm_request import LlmRequest
            from google.genai.types import Content, Part

            # Build LLM request using ADK types
            llm_request = LlmRequest(
                contents=[
                    Content(
                        role="user",
                        parts=[Part(text=prompt.format(content=content))],
                    )
                ],
            )

            # Call the model directly
            logger.debug("Summarizing session content using model %s", model.model)
            response_generator = model.generate_content_async(llm_request, stream=False)

            # Consume the async generator (streaming response)
            summary_text = ""
            async for chunk in response_generator:
                if chunk.content and chunk.content.parts:
                    summary_text += "".join(
                        part.text for part in chunk.content.parts if hasattr(part, "text") and part.text
                    )

            summary_text = summary_text.strip()

            if summary_text:
                # Clean up potential markdown formatting if model ignores instruction
                if summary_text.startswith("```json"):
                    summary_text = summary_text[7:]
                if summary_text.startswith("```"):
                    summary_text = summary_text[3:]
                if summary_text.endswith("```"):
                    summary_text = summary_text[:-3]
                summary_text = summary_text.strip()

                try:
                    extracted_list = json.loads(summary_text)
                    if isinstance(extracted_list, list) and all(isinstance(item, str) for item in extracted_list):
                        logger.debug("Summarized session content into %d items", len(extracted_list))
                        return extracted_list
                    else:
                        logger.warning("LLM returned valid JSON but not a list of strings. Falling back to full text.")
                except json.JSONDecodeError:
                    logger.warning(
                        "Failed to parse LLM output as JSON. Falling back to full text. Output: %s", summary_text
                    )
                    pass

            logger.warning("Empty summary or invalid format returned, using original content")
            return [content]

        except Exception as e:
            logger.warning("Failed to summarize session content: %s. Using original.", e)
            return [content]
