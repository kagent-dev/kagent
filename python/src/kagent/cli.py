import logging

import os
import typer
from mcp.server.fastmcp import FastMCP
from autogen_core import ROOT_LOGGER_NAME

app = typer.Typer()

mcp = FastMCP("KAgent")


@app.command()
def serve(
    host: str = "127.0.0.1",
    port: int = 8081,
    reload: bool = False,
):
    import logging
    import os

    from opentelemetry import trace
    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
    from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
    from opentelemetry.instrumentation.openai import OpenAIInstrumentor
    from opentelemetry.sdk.resources import Resource
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor

    from autogenstudio.cli import ui

    tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
    if tracing_enabled:
        logging.info("Enabling tracing")
        tracer_provider = TracerProvider(resource=Resource({"service.name": "kagent"}))
        processor = BatchSpanProcessor(OTLPSpanExporter())
        tracer_provider.add_span_processor(processor)
        trace.set_tracer_provider(tracer_provider)
        HTTPXClientInstrumentor().instrument()
        OpenAIInstrumentor().instrument()

    ui(host=host, port=port, reload=reload)


def run():
    LOG_LEVEL = os.getenv("LOG_LEVEL", "INFO").upper()
    logging.basicConfig(level=LOG_LEVEL)
    logger = logging.getLogger(ROOT_LOGGER_NAME)
    logger.setLevel(LOG_LEVEL)
    app()


if __name__ == "__main__":
    run()
