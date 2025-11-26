import logging
from contextlib import asynccontextmanager
from typing import Any


@asynccontextmanager
async def lifespan(app: Any):
    logging.info("Lifespan: setup")
    try:
        yield
    finally:
        logging.info("Lifespan: teardown")
