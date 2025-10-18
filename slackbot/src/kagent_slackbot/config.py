import os
from dataclasses import dataclass

from dotenv import load_dotenv

load_dotenv()


@dataclass
class SlackConfig:
    """Slack-specific configuration"""

    bot_token: str
    app_token: str
    signing_secret: str


@dataclass
class KagentConfig:
    """Kagent API configuration"""

    base_url: str
    timeout: int = 30


@dataclass
class ServerConfig:
    """HTTP server configuration"""

    host: str = "0.0.0.0"
    port: int = 8080


@dataclass
class Config:
    """Main application configuration"""

    slack: SlackConfig
    kagent: KagentConfig
    server: ServerConfig
    permissions_file: str = "config/permissions.yaml"
    log_level: str = "INFO"


def load_config() -> Config:
    """Load configuration from environment variables"""

    # Required variables
    required = [
        "SLACK_BOT_TOKEN",
        "SLACK_APP_TOKEN",
        "SLACK_SIGNING_SECRET",
        "KAGENT_BASE_URL",
    ]

    missing = [var for var in required if not os.getenv(var)]
    if missing:
        raise ValueError(f"Missing required environment variables: {', '.join(missing)}")

    return Config(
        slack=SlackConfig(
            bot_token=os.environ["SLACK_BOT_TOKEN"],
            app_token=os.environ["SLACK_APP_TOKEN"],
            signing_secret=os.environ["SLACK_SIGNING_SECRET"],
        ),
        kagent=KagentConfig(
            base_url=os.environ["KAGENT_BASE_URL"],
            timeout=int(os.getenv("KAGENT_TIMEOUT", "30")),
        ),
        server=ServerConfig(
            host=os.getenv("SERVER_HOST", "0.0.0.0"),
            port=int(os.getenv("SERVER_PORT", "8080")),
        ),
        permissions_file=os.getenv("PERMISSIONS_FILE", "config/permissions.yaml"),
        log_level=os.getenv("LOG_LEVEL", "INFO"),
    )
