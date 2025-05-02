import asyncio
from typing import Any, List, Optional, Sequence

from autogen_core import CancellationToken, Component
from autogen_core.memory import Memory, MemoryContent, MemoryMimeType, MemoryQueryResult, UpdateContextResult
from autogen_core.model_context import ChatCompletionContext
from autogen_core.models import SystemMessage
from loguru import logger
from pinecone import Pinecone
from pinecone.core.openapi.db_data.model.hit import Hit
from pinecone.data import Index
from pydantic import BaseModel, Field, SecretStr
from typing_extensions import Self


class PineconeMemoryConfig(BaseModel):
    api_key: SecretStr = Field(..., description="The API key for the Pinecone API")
    index_host: str = Field(..., description="The host for the Pinecone index")
    top_k: int = Field(default=5, description="The number of results to retrieve from Pinecone")
    namespace: Optional[str] = Field(default=None, description="The Pinecone namespace to query")
    record_fields: Optional[List[str]] = Field(description="The fields to retrieve from the Pinecone index")
    score_threshold: float = Field(
        default=0.0,
        description="The score threshold of results to include in the context. Results with a score below this threshold will be ignored.",
    )


class PineconeMemory(Memory, Component[PineconeMemoryConfig]):
    component_config_schema = PineconeMemoryConfig
    component_type = "memory"
    component_provider_override = "kagent.memory.PineconeMemory"

    def __init__(self, config: PineconeMemoryConfig):
        self._config = config
        self._pc: Pinecone | None = None
        self._index: Index | None = None
        self._initialize()

    def _initialize(self):
        """Initialize Pinecone if not already done."""
        if self._pc is None:
            try:
                self._pc = Pinecone(api_key=self._config.api_key.get_secret_value(), host=self._config.index_host)
                self._index = self._pc.Index(host=self._config.index_host)
            except Exception as e:
                logger.error(f"Failed to initialize Pinecone: {e}")
                raise Exception(f"Failed to initialize Pinecone: {e}") from e

    async def update_context(
        self,
        model_context: ChatCompletionContext,
        cancellation_token: CancellationToken | None = None,
    ) -> UpdateContextResult:
        """Update the context by querying Pinecone based on the last message.

        Retrieves relevant text chunks from Pinecone and adds them as a SystemMessage
        to the beginning of the context.
        """
        messages = await model_context.get_messages()
        if not messages:
            return UpdateContextResult(success=True, memories=MemoryQueryResult(results=[]))

        # Use the last message as the query basis
        last_message = messages[-1]
        query_text = last_message.content if isinstance(last_message.content, str) else str(last_message)

        if not query_text:
            return UpdateContextResult(success=True, memories=MemoryQueryResult(results=[]))

        try:
            query_results = await self.query(query_text, cancellation_token=cancellation_token)
            if query_results.results:
                memory_strings = [f"{i}. {str(memory.content)}" for i, memory in enumerate(query_results.results, 1)]
                memory_context = "\nYour response should include the following memory content:\n" + "\n".join(
                    memory_strings
                )

                await model_context.add_message(SystemMessage(content=memory_context))

            return UpdateContextResult(success=True, memories=query_results)
        except Exception as e:
            logger.error(f"Error during Pinecone update_context: {e}")
            return UpdateContextResult(success=False, error=str(e), memories=MemoryQueryResult(results=[]))

    async def query(
        self,
        query: str | MemoryContent,
        cancellation_token: CancellationToken | None = None,
        **kwargs: Any,
    ) -> MemoryQueryResult:
        """Query the memory with a specific string or MemoryContent."""
        query_text = ""
        if isinstance(query, str):
            query_text = query
        elif isinstance(query, MemoryContent) and query.mime_type == MemoryMimeType.TEXT:
            query_text = query.content
        else:
            logger.error("Query must be a string or text MemoryContent.")
            raise ValueError("Query must be a string or text MemoryContent.")

        if not query_text:
            return MemoryQueryResult(results=[])

        if cancellation_token and cancellation_token.is_cancelled:
            logger.info("Query cancelled.")
            return MemoryQueryResult(results=[])

        try:
            query_response = await asyncio.to_thread(
                self._index.search,
                namespace=self._config.namespace,
                query={"inputs": {"text": query_text}, "top_k": self._config.top_k},
                fields=self._config.record_fields,
            )

            results: List[MemoryContent] = []
            if query_response and "result" in query_response:
                for match in query_response.result.hits:
                    hit: Hit = match
                    score = hit.get("_score")
                    # Ignore hits with a score below the threshold
                    if score and score < self._config.score_threshold:
                        continue

                    if not self._config.record_fields:
                        # if no record fields are specified initially, use all fields
                        logger.info("No record fields specified, using all fields.")
                        self._config.record_fields = list(hit.fields.keys())

                    for field in self._config.record_fields:
                        # For each hit, we get the text from record_fields, and store the remaining fields in metadata
                        text = hit.fields.get(field)
                        metadata = {k: v for k, v in hit.fields.items() if k != field}
                        results.append(MemoryContent(content=text, mime_type=MemoryMimeType.TEXT, metadata=metadata))

            if len(results) == 0:
                logger.warning("No results found from Pinecone query.")

            return MemoryQueryResult(results=results)
        except Exception as e:
            logger.error(f"Error during Pinecone query: {e}")
            raise e

    async def add(
        self, content: MemoryContent | Sequence[MemoryContent], cancellation_token: CancellationToken | None = None
    ) -> None:
        pass

    async def reset(self) -> None:
        """Reset the memory by deleting all data in the specified namespace (or the whole index if no namespace)."""
        pass

    async def close(self) -> None:
        """Clean up Pinecone client and resources."""
        self._pc = None
        self._index = None
        logger.info("PineconeMemory closed.")

    async def clear(self) -> None:
        """Clear the memory by deleting all data in the specified namespace (or the whole index if no namespace)."""
        pass

    def _to_config(self) -> PineconeMemoryConfig:
        """Serialize the memory configuration."""
        return self._config

    @classmethod
    def _from_config(cls, config: PineconeMemoryConfig) -> Self:
        """Deserialize the memory configuration."""
        return cls(config=config)
