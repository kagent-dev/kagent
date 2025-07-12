# api/app.py
import os
from contextlib import asynccontextmanager
from typing import AsyncGenerator

# import logging
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles
from loguru import logger

from ..version import VERSION
from .config import settings
from .routes import (
    invoke,
    models,
    tool_servers,
    validation,
)

# Initialize application

# Create FastAPI application
app = FastAPI(debug=True)

# CORS middleware configuration
app.add_middleware(
    CORSMiddleware,
    allow_origins=[
        "http://localhost:8000",
        "http://127.0.0.1:8000",
        "http://localhost:8001",
        "http://localhost:8081",
    ],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Create API router with version and documentation
api = FastAPI(
    root_path="/api",
    title="AutoGen Studio API",
    version=VERSION,
    description="AutoGen Studio is a low-code tool for building and testing multi-agent workflows.",
    docs_url="/docs" if settings.API_DOCS else None,
)

# Include all routers with their prefixes
api.include_router(
    validation.router,
    prefix="/validate",
    tags=["validation"],
    responses={404: {"description": "Not found"}},
)

api.include_router(
    tool_servers.router,
    prefix="/toolservers",
    tags=["tool servers"],
    responses={404: {"description": "Not found"}},
)

api.include_router(
    models.router,
    prefix="/models",
    tags=["models"],
    responses={404: {"description": "Not found"}},
)

api.include_router(
    invoke.router,
    prefix="/invoke",
    tags=["invoke"],
    responses={404: {"description": "Not found"}},
)

# Version endpoint


@api.get("/version")
async def get_version():
    """Get API version"""
    return {
        "status": True,
        "message": "Version retrieved successfully",
        "data": {"version": VERSION},
    }


# Health check endpoint


@api.get("/health")
async def health_check():
    """API health check endpoint"""
    return {
        "status": True,
        "message": "Service is healthy",
    }


app.mount("/api", api)
# Error handlers


@app.exception_handler(500)
async def internal_error_handler(request, exc):
    logger.error(f"Internal error: {str(exc)}")
    return {
        "status": False,
        "message": "Internal server error",
        "detail": "Internal server error",
    }


def create_app() -> FastAPI:
    """
    Factory function to create and configure the FastAPI application.
    Useful for testing and different deployment scenarios.
    """
    return app
