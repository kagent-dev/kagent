from typing import Optional


class STSError(Exception):
    """Base exception for STS client errors."""

    pass


class TokenExchangeError(STSError):
    """Exception raised when token exchange fails."""

    def __init__(self, error: str, error_description: Optional[str] = None, status_code: Optional[int] = None):
        self.error = error
        self.error_description = error_description
        self.status_code = status_code
        super().__init__(f"Token exchange failed: {error} - {error_description}")


class ConfigurationError(STSError):
    """Exception raised when STS configuration is invalid."""

    pass


class AuthenticationError(STSError):
    """Exception raised when authentication fails."""

    pass


class NetworkError(STSError):
    """Exception raised when network operations fail."""

    pass
