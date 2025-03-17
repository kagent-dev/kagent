import logging
import os
import sqlite3
from pathlib import Path
from typing import Any, Dict, List, Optional, Type

import numpy as np
import requests
import sqlite_vec
from autogen_core import CancellationToken, Component
from autogen_core.tools import BaseTool
from openai import OpenAI
from pydantic import BaseModel, Field

# Default base URL for downloading the documentation database
DEFAULT_DB_URL = "https://doc-sqlite-db.s3.sa-east-1.amazonaws.com"

# Map of supported products and their database files
PRODUCT_DB_MAP = {
    "kubernetes": "kubernetes.db",
    "istio": "istio.db",
    "argo cd": "argo.db",
    "argo rollouts": "argo-rollouts.db",
    "helm": "helm.db",
    "prometheus": "prometheus.db",
    "gateway-api": "gateway-api.db",
    "gloo-gateway": "gloo-gateway.db",
    "gloo-edge": "gloo-edge.db",
    "otel": "otel.db",
    "cilium": "cilium.db",
    "istio ambient mesh": "ambient.db",
}


class Config(BaseModel):
    """Configuration for Documentation search tools."""

    docs_base_path: Optional[str] = Field(
        default="", description="Base path for the documentation database. If empty, the database will be downloaded."
    )
    docs_download_url: Optional[str] = Field(
        default=DEFAULT_DB_URL,
        description="Base URL for downloading the documentation database. If empty, the default URL will be used.",
    )
    openai_api_key: Optional[str] = Field(
        default=None,
        description="API key for OpenAI services. If empty, the environment variable 'OPENAI_API_KEY' will be used.",
    )


class QueryResult:
    def __init__(self, chunk_id, distance, content, url=None, **kwargs):
        self.chunk_id = chunk_id
        self.distance = distance
        self.content = content
        self.url = url
        for key, value in kwargs.items():
            setattr(self, key, value)


class SQLiteDownloader:
    def __init__(self, base_url: str, product_map: Dict[str, str]):
        self.base_url = base_url
        self.product_map = product_map
        self._db_cache = {}

    def validate_product(self, product_name: str) -> None:
        """Validate if the product is supported."""
        if not product_name:
            raise ValueError("Product name cannot be empty")
        if product_name not in self.product_map:
            raise ValueError(
                f"Unsupported product: {product_name}. Supported products: {', '.join(self.product_map.keys())}"
            )

    def get_db_path(self, product_name: str) -> Path:
        """Get local path for cached database."""
        self.validate_product(product_name)
        cache_dir = Path.home() / ".kagent/cache" / "doc-query"
        cache_dir.mkdir(parents=True, exist_ok=True)
        return cache_dir / self.product_map[product_name]

    def download_if_needed(self, product_name: str) -> Path:
        """Download the SQLite database if not in cache."""
        self.validate_product(product_name)
        db_path = self.get_db_path(product_name)

        if not db_path.exists():
            db_filename = self.product_map[product_name]
            db_url = f"{self.base_url}/{db_filename}"

            try:
                logging.debug(f"Downloading database for {product_name} from {db_url}")
                response = requests.get(db_url, stream=True)
                response.raise_for_status()

                with open(db_path, "wb") as f:
                    for chunk in response.iter_content(chunk_size=8192):
                        f.write(chunk)
                logging.debug(f"Successfully downloaded database for {product_name}")

            except requests.exceptions.RequestException as e:
                logging.error(f"Error downloading database for {product_name}: {e}")
                raise

        return db_path


class QueryInput(BaseModel):
    query: str = Field(
        description="The search query to use for finding relevant documentation. Be specific and include relevant keywords."
    )
    product_name: str = Field(
        description=f"The name of the product to search within. Examples include: {', '.join(PRODUCT_DB_MAP.keys())}"
    )
    version: Optional[str] = Field(
        default=None,
        description="The specific version of the product documentation to search. Use the full version number without the 'v' prefix.",
    )
    limit: Optional[int] = Field(
        default=4, description="Optional. The maximum number of search results to return. Defaults to 4."
    )


class QueryTool(BaseTool, Component[Config]):
    """Tool for querying documentation for a specific product."""

    component_type = "tool"
    component_config_schema = Config
    component_provider_override = "kagent.tools.docs.QueryTool"
    _description = f"Searches a vector database for relevant documentation related to one of these projects: {', '.join(PRODUCT_DB_MAP.keys())}"

    def __init__(self, config: Config) -> None:
        # Initialize SQLite downloader with base S3 URL and product mapping if override is not provided
        self.db_downloader = SQLiteDownloader(
            base_url=config.docs_download_url or os.environ.get("DB_BASE_URL", DEFAULT_DB_URL),
            product_map=PRODUCT_DB_MAP,
        )
        # Initialize OpenAI with API key from config
        api_key = config.openai_api_key or os.environ.get("OPENAI_API_KEY")
        self.openai = OpenAI(api_key=api_key)
        self.config: Config = config

        super().__init__(QueryInput, BaseModel, "query_tool", description=self._description)

    async def run(self, args: QueryInput, cancellation_token: CancellationToken) -> Any:
        db_path = self.config.docs_base_path

        if cancellation_token.is_cancelled():
            raise Exception("Operation cancelled")

        if db_path == "":
            try:
                db_path = self.db_downloader.download_if_needed(args.product_name)
            except Exception as e:
                logging.error(f"Failed to get database: {e}")
                raise
        return self.query_documentation(args.query, args.product_name, args.version, args.limit, db_path)

    def _to_config(self) -> Config:
        """Convert to config object."""
        return self.config.copy()

    @classmethod
    def _from_config(cls, config: Config) -> "QueryTool":
        return cls(config)

    def create_embeddings(self, text: str) -> List[float]:
        response = self.openai.embeddings.create(model="text-embedding-3-large", input=text)
        return response.data[0].embedding

    def query_collection(
        self, query_embedding: List[float], filter: dict, top_k: int = 10, db_path: str | None = None
    ) -> List[QueryResult]:
        conn = sqlite3.connect(str(db_path))
        conn.enable_load_extension(True)
        sqlite_vec.load(conn)
        conn.enable_load_extension(False)

        cursor = conn.cursor()

        query = """SELECT *, distance FROM vec_items WHERE embedding MATCH ?"""
        params = [np.array(query_embedding, dtype=np.float32).tobytes()]

        if "product_name" in filter:
            query += " AND product_name = ?"
            params.append(filter["product_name"])

        if "version" in filter:
            query += " AND version = ?"
            params.append(filter["version"])

        query += " ORDER BY distance LIMIT ?;"
        params.append(top_k)

        cursor.execute(query, params)
        rows = cursor.fetchall()
        conn.close()

        results = []
        for row in rows:
            row_dict = dict(zip([column[0] for column in cursor.description], row, strict=True))
            row_dict.pop("embedding", None)
            results.append(QueryResult(**row_dict))

        return results

    def query_documentation(
        self, query_text: str, product_name: str, version: str | None = None, limit: int = 4, db_path: str | None = None
    ) -> List[Dict[str, Any]]:
        """Query documentation for a specific product."""
        if query_text == "" or product_name == "":
            raise ValueError("Both query and product must be specified")

        try:
            if product_name not in PRODUCT_DB_MAP:
                raise ValueError(
                    f"Unsupported product: {product_name}. Supported product: {', '.join(PRODUCT_DB_MAP.keys())}"
                )

            version = version if version and version.strip() else None

            query_embedding = self.create_embeddings(query_text)

            filter = {"product_name": product_name}
            if version:
                filter["version"] = version

            results = self.query_collection(query_embedding, filter, limit, db_path)
            return [{"distance": qr.distance, "content": qr.content} for qr in results]

        except Exception as e:
            logging.error("An error occurred: %s", e)
            raise

    def list_supported_product(self) -> List[str]:
        """Return a list of supported products."""
        return list(PRODUCT_DB_MAP.keys())
