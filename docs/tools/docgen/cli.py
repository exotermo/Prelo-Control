"""CLI: normalizes a Markdown document (with Mermaid diagrams) into a PDF with a cover.

Usage:
    python -m docgen.cli --input DOC.md --output DOC.pdf \\
        [--brochure-dir DIR] [--title T] [--subtitle S] [--version V] \\
        [--author A] [--date D] [--var KEY=VALUE ...] [--no-mermaid] [--keep-temp]

The first H1 heading of the Markdown file, and the "Versão" line in its cover block, are used
as fallback --title / --version when those flags are omitted.
"""
from __future__ import annotations

import argparse
import re
import shutil
import sys
import tempfile
from datetime import date
from pathlib import Path

from . import mermaid
from .brochure import DEFAULT_BROCHURE_DIR, BrochureContext, render_back_cover_pdf, render_cover_pdf
from .markdown_render import render_body_pdf
from .pdf import merge_pdfs

_TITLE_RE = re.compile(r"^\*\*([^*]+)\*\*\s*$", re.MULTILINE)
_VERSION_RE = re.compile(r"Vers[ãa]o\s+([0-9][0-9.]*)", re.IGNORECASE)


def _guess_title_and_version(markdown_text: str) -> tuple[str, str]:
    """Best-effort fallback: the PGP-style cover block's first two bold lines are
    "**SIGLA**" and "**Nome do Documento**"; the third is typically "**Versão X.Y**".
    """
    bold_lines = _TITLE_RE.findall(markdown_text[:1000])
    title = bold_lines[1] if len(bold_lines) > 1 else (bold_lines[0] if bold_lines else "Documento")
    version_match = _VERSION_RE.search(markdown_text[:1000])
    version = version_match.group(1) if version_match else "1.0"
    return title, version


def _parse_vars(pairs: list[str]) -> dict:
    extra = {}
    for pair in pairs:
        if "=" not in pair:
            raise argparse.ArgumentTypeError(f"--var expects KEY=VALUE, got {pair!r}")
        key, value = pair.split("=", 1)
        extra[key] = value
    return extra


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--input", required=True, type=Path, help="Path to the source .md file")
    parser.add_argument("--output", required=True, type=Path, help="Path for the generated .pdf file")
    parser.add_argument(
        "--brochure-dir", type=Path, default=None,
        help="Directory with cover.html.j2 (and optional back_cover.html.j2, style.css, assets/). "
             "Defaults to docs/tools/brochures/default.",
    )
    parser.add_argument("--title", default=None, help="Overrides the cover title (else guessed from the .md)")
    parser.add_argument("--subtitle", default="")
    parser.add_argument("--brand", default="", help="Short brand/project mark shown above the title (e.g. a project codename)")
    parser.add_argument("--version", default=None, help="Overrides the cover version (else guessed from the .md)")
    parser.add_argument("--author", default="")
    parser.add_argument("--date", default=None, help="Defaults to today (ISO format)")
    parser.add_argument("--var", action="append", default=[], metavar="KEY=VALUE",
                         help="Extra brochure template variable, repeatable")
    parser.add_argument("--no-mermaid", action="store_true", help="Skip rendering ```mermaid blocks to images")
    parser.add_argument("--keep-temp", action="store_true", help="Keep the working directory for inspection")
    return parser


def run(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)

    if not args.input.exists():
        print(f"error: input file not found: {args.input}", file=sys.stderr)
        return 1

    markdown_text = args.input.read_text(encoding="utf-8")
    guessed_title, guessed_version = _guess_title_and_version(markdown_text)
    context = BrochureContext(
        title=args.title or guessed_title,
        subtitle=args.subtitle,
        version=args.version or guessed_version,
        author=args.author,
        date=args.date or date.today().isoformat(),
        extra={"brand": args.brand, **_parse_vars(args.var)},
    )
    brochure_dir = args.brochure_dir or DEFAULT_BROCHURE_DIR

    work_dir = Path(tempfile.mkdtemp(prefix="docgen-"))
    try:
        body_markdown = markdown_text
        if not args.no_mermaid and mermaid.find_blocks(markdown_text):
            body_markdown, blocks = mermaid.render_all(markdown_text, work_dir)
            print(f"rendered {len(blocks)} mermaid diagram(s)", file=sys.stderr)

        body_pdf = render_body_pdf(body_markdown, work_dir / "body.pdf", base_url=str(work_dir))
        cover_pdf = render_cover_pdf(brochure_dir, context, work_dir / "cover.pdf")
        back_cover_pdf = render_back_cover_pdf(brochure_dir, context, work_dir / "back_cover.pdf")

        args.output.parent.mkdir(parents=True, exist_ok=True)
        merge_pdfs(args.output, cover_pdf, body_pdf, back_cover_pdf)
        print(f"wrote {args.output}", file=sys.stderr)
        return 0
    finally:
        if args.keep_temp:
            print(f"kept working directory: {work_dir}", file=sys.stderr)
        else:
            shutil.rmtree(work_dir, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(run())
