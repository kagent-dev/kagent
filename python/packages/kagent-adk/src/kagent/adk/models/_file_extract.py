"""Helpers to turn uploaded file blobs into text the model can read.

Rich documents (PDF, DOCX, XLSX, PPTX, HTML) are extracted to markdown via
``markitdown``; text-like files are returned as-is. This mirrors the Go ADK
runtime behaviour so non-image uploads reach the model instead of being dropped.
"""

from __future__ import annotations

import os
import tempfile
from typing import Optional

from google.genai import types

# Rich document MIME types mapped to the extension markitdown uses for format
# detection. Kept in sync with the Go runtime (fileextract.go): the common
# formats both markitdown (Python) and tabula (Go) support — PDF, DOCX, XLSX,
# PPTX, HTML, EPUB.
_DOC_MIME_TO_EXT: dict[str, str] = {
    "application/pdf": ".pdf",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx",
    "application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
    "application/epub+zip": ".epub",
    "text/html": ".html",
}

# Filename extensions markitdown can extract as rich documents.
_DOC_EXTS: dict[str, str] = {
    ".pdf": ".pdf",
    ".docx": ".docx",
    ".xlsx": ".xlsx",
    ".pptx": ".pptx",
    ".epub": ".epub",
    ".html": ".html",
    ".htm": ".html",
}


def _normalize_mime(mime_type: Optional[str]) -> str:
    if not mime_type:
        return ""
    mime_type = mime_type.strip().lower()
    if ";" in mime_type:
        mime_type = mime_type.split(";", 1)[0].strip()
    return mime_type


def _is_text_like(mime_type: Optional[str]) -> bool:
    """Whether a MIME type can be inlined as raw text (text/html excluded)."""
    mime_type = _normalize_mime(mime_type)
    if mime_type == "text/html":
        return False
    if mime_type.startswith("text/"):
        return True
    return mime_type in {
        "application/json",
        "application/xml",
        "application/x-ndjson",
        "application/yaml",
        "application/x-yaml",
    }


def _doc_ext(mime_type: Optional[str], name: Optional[str]) -> str:
    """Extension to use for markitdown, derived from filename then MIME type."""
    if name:
        _, ext = os.path.splitext(name.lower())
        if ext in _DOC_EXTS:
            return _DOC_EXTS[ext]
    return _DOC_MIME_TO_EXT.get(_normalize_mime(mime_type), "")


def _extract_doc_text(data: bytes, ext: str) -> str:
    """Write bytes to a temp file (markitdown detects by extension) and extract."""
    # Lazy import so the package still imports if markitdown is unavailable.
    from markitdown import MarkItDown

    fd, tmp_path = tempfile.mkstemp(suffix=ext)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
        result = MarkItDown().convert(tmp_path)
        return result.text_content or ""
    finally:
        os.remove(tmp_path)


def extract_file_text(data: bytes, mime_type: Optional[str], name: Optional[str]) -> str:
    """Extract readable text from an uploaded file's bytes.

    Raises ValueError for formats that cannot be represented as text.
    """
    ext = _doc_ext(mime_type, name)
    if ext:
        return _extract_doc_text(data, ext)
    if _is_text_like(mime_type):
        return data.decode("utf-8", errors="replace")
    raise ValueError(f"unsupported file type for text extraction: mime={mime_type!r} name={name!r}")


def inline_file_to_text(blob: types.Blob) -> Optional[str]:
    """Convert a non-image inline file blob into text for a chat message.

    Returns ``None`` for an empty blob. On extraction failure returns a short
    note so the model can tell the user the file could not be read, instead of
    silently dropping it.
    """
    if blob is None or not blob.data:
        return None
    name = blob.display_name or "file"
    mime_type = blob.mime_type or ""
    try:
        text = extract_file_text(blob.data, mime_type, name)
    except Exception:
        return f'[Uploaded file "{name}" ({mime_type}) could not be read as text.]'
    if not text.strip():
        return f'[Uploaded file "{name}" ({mime_type}) contained no extractable text.]'
    return f'Contents of uploaded file "{name}":\n\n{text}'
