"""Project settings."""
import logging

from pydantic import computed_field
from pydantic_settings import BaseSettings, SettingsConfigDict

logging.basicConfig(level=logging.DEBUG)
centrifuge_logger = logging.getLogger("centrifuge")

class Settings(BaseSettings):
    """Project settings."""

    CENTRIFUGO_URL: str = "localhost:8000"

    @computed_field
    @property
    def centrifugo_connection_url(self) -> str:
        """Centrifugo websocket connection url."""
        return f"ws://{settings.CENTRIFUGO_URL}/connection/websocket"

    model_config = SettingsConfigDict()


settings = Settings()
