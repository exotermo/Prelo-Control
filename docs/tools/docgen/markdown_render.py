"""Markdown -> HTML -> PDF for the document body (not the cover, see brochure.py)."""
from __future__ import annotations

from pathlib import Path

import markdown as md_lib
from weasyprint import CSS, HTML

_EXTENSIONS = ["tables", "fenced_code", "toc", "attr_list", "sane_lists", "nl2br"]

_BODY_CSS = """
@page {
    size: A4;
    margin: 2.2cm 2cm 2.4cm 2cm;
    @bottom-center { content: counter(page); font-size: 9pt; color: #666; }
}
body { font-family: "DejaVu Sans", Arial, sans-serif; font-size: 10.5pt; line-height: 1.45; color: #1a1a1a; }
h1 { font-size: 18pt; margin-top: 1.4em; border-bottom: 2px solid #1f2a44; padding-bottom: 0.2em; }
h2 { font-size: 14pt; margin-top: 1.2em; color: #1f2a44; }
h3 { font-size: 11.5pt; margin-top: 1em; color: #2b3a5e; }
h1, h2, h3 { page-break-after: avoid; }
table { border-collapse: collapse; width: 100%; margin: 0.8em 0; font-size: 9.5pt; }
th, td { border: 1px solid #ccc; padding: 5px 8px; text-align: left; vertical-align: top; }
th { background: #1f2a44; color: #fff; }
tr:nth-child(even) td { background: #f5f6fa; }
code { background: #f0f0f0; padding: 1px 4px; border-radius: 3px; font-size: 92%; }
pre { background: #f0f0f0; padding: 8px; border-radius: 4px; overflow-x: auto; }
img { max-width: 100%; page-break-inside: avoid; }
blockquote { border-left: 3px solid #1f2a44; margin: 0.6em 0; padding: 0.2em 0 0.2em 1em; color: #444; }
a { color: #1f2a44; }
hr { border: none; border-top: 1px solid #ccc; margin: 1.4em 0; }
"""


def markdown_to_html(markdown_text: str) -> str:
    body = md_lib.markdown(markdown_text, extensions=_EXTENSIONS)
    return f"<!DOCTYPE html><html><head><meta charset='utf-8'></head><body>{body}</body></html>"


def render_body_pdf(markdown_text: str, output_path: Path, base_url: str, extra_css: str | None = None) -> Path:
    """Renders the document body (everything after the cover) to a standalone PDF."""
    html_text = markdown_to_html(markdown_text)
    css = _BODY_CSS + (extra_css or "")
    HTML(string=html_text, base_url=base_url).write_pdf(str(output_path), stylesheets=[CSS(string=css)])
    return output_path
