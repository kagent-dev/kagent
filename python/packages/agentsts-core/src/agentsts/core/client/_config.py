from pydantic import BaseModel, Field


class STSConfig(BaseModel):
    """Configuration for STS client."""

    well_known_uri: str = Field(..., description="The well-known configuration URI")
    timeout: int = Field(default=5, description="Request timeout in seconds")
    verify_ssl: bool = Field(default=True, description="Whether to verify SSL certificates")
    use_issuer_host: bool = Field(
        default=False,
        description="Replace the host:port in token_endpoint with the host:port from well_known_uri",
    )
