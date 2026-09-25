"""Merges cover / body / back-cover PDFs, in that order, into a single final PDF."""
from __future__ import annotations

from pathlib import Path

from pypdf import PdfWriter


def merge_pdfs(output_path: Path, *parts: Path | None) -> Path:
    writer = PdfWriter()
    for part in parts:
        if part is None:
            continue
        writer.append(str(part))
    with open(output_path, "wb") as fh:
        writer.write(fh)
    return output_path
