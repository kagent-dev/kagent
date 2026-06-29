"""Tests for uploaded-file text extraction (_file_extract)."""

import pytest
from google.genai import types

from kagent.adk.models._file_extract import (
    _doc_ext,
    _is_text_like,
    extract_file_text,
    inline_file_to_text,
)


@pytest.mark.parametrize(
    "mime,want",
    [
        ("text/plain", True),
        ("text/markdown", True),
        ("text/csv", True),
        ("application/json", True),
        ("text/plain; charset=utf-8", True),
        ("text/html", False),  # routed through extraction instead
        ("application/pdf", False),
        ("image/png", False),
    ],
)
def test_is_text_like(mime, want):
    assert _is_text_like(mime) is want


@pytest.mark.parametrize(
    "mime,name,want",
    [
        ("application/pdf", "report", ".pdf"),
        ("application/octet-stream", "report.pdf", ".pdf"),
        (
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            "x",
            ".docx",
        ),
        ("", "page.htm", ".html"),
        ("text/plain", "a.txt", ""),
        ("application/zip", "a.zip", ""),
    ],
)
def test_doc_ext(mime, name, want):
    assert _doc_ext(mime, name) == want


@pytest.mark.parametrize(
    "data,mime,name,want",
    [
        (b"hello world", "text/plain", "a.txt", "hello world"),
        (b'{"k":"v"}', "application/json", "a.json", '{"k":"v"}'),
        (b"a,b\n1,2", "text/csv", "a.csv", "a,b\n1,2"),
    ],
)
def test_extract_file_text_text_like(data, mime, name, want):
    assert extract_file_text(data, mime, name) == want


def test_extract_file_text_unsupported_raises():
    with pytest.raises(ValueError):
        extract_file_text(b"\x00\x01\x02", "application/zip", "a.zip")


def test_inline_file_to_text_none_for_empty():
    assert inline_file_to_text(types.Blob(data=b"", mime_type="text/plain")) is None


def test_inline_file_to_text_labels_text_file():
    out = inline_file_to_text(types.Blob(data=b"line1\nline2", mime_type="text/plain", display_name="notes.txt"))
    assert out is not None
    assert 'Contents of uploaded file "notes.txt"' in out
    assert "line1" in out


def test_inline_file_to_text_note_on_unsupported():
    out = inline_file_to_text(types.Blob(data=b"\x00\x01", mime_type="application/zip", display_name="a.zip"))
    assert out is not None
    assert "could not be read as text" in out


def test_extract_file_text_html_via_markitdown():
    """Exercises the real markitdown extraction path (skipped if not installed)."""
    pytest.importorskip("markitdown")
    html = b"<html><body><h1>Invoice</h1><p>Total: 42 USD</p></body></html>"
    out = extract_file_text(html, "text/html", "invoice.html")
    assert "Invoice" in out
    assert "42 USD" in out
