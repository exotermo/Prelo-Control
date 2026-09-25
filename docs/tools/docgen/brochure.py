"""Renders a cover ("brochure") PDF from a swappable template directory.

A brochure directory is a small, self-contained theme:
    cover.html.j2        (required) Jinja2 template for the cover page
    back_cover.html.j2    (optional) Jinja2 template for a closing page, appended after the body
    style.css             (optional) extra CSS merged into the cover's stylesheet
    assets/                (optional) images referenced by the templates (logo, etc.)

docs/tools/brochures/default/ ships a ready-to-use brochure in this same shape, used when the
caller passes no --brochure-dir.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

from jinja2 import Environment, FileSystemLoader, select_autoescape
from weasyprint import CSS, HTML

_HERE = Path(__file__).resolve().parent
DEFAULT_BROCHURE_DIR = _HERE.parent / "brochures" / "default"

_COVER_BASE_CSS = """
@page { size: A4; margin: 0; }
body { margin: 0; font-family: "DejaVu Sans", Arial, sans-serif; }
"""


@dataclass(frozen=True)
class BrochureContext:
    """Template variables available to cover.html.j2 / back_cover.html.j2 as `doc.*`."""

    title: str
    subtitle: str = ""
    version: str = "1.0"
    author: str = ""
    date: str = ""
    extra: dict = field(default_factory=dict)

    def as_dict(self) -> dict:
        return {
            "title": self.title,
            "subtitle": self.subtitle,
            "version": self.version,
            "author": self.author,
            "date": self.date,
            **self.extra,
        }


class BrochureError(RuntimeError):
    pass


def _render_template(brochure_dir: Path, template_name: str, context: BrochureContext) -> str | None:
    template_path = brochure_dir / template_name
    if not template_path.exists():
        return None
    env = Environment(loader=FileSystemLoader(str(brochure_dir)), autoescape=select_autoescape(["html"]))
    template = env.get_template(template_name)
    return template.render(doc=context.as_dict())


def render_cover_pdf(brochure_dir: Path, context: BrochureContext, output_path: Path) -> Path:
    html_text = _render_template(brochure_dir, "cover.html.j2", context)
    if html_text is None:
        raise BrochureError(f"{brochure_dir} has no cover.html.j2")
    css_path = brochure_dir / "style.css"
    stylesheets = [CSS(string=_COVER_BASE_CSS)]
    if css_path.exists():
        stylesheets.append(CSS(filename=str(css_path)))
    HTML(string=html_text, base_url=str(brochure_dir)).write_pdf(str(output_path), stylesheets=stylesheets)
    return output_path


def render_back_cover_pdf(brochure_dir: Path, context: BrochureContext, output_path: Path) -> Path | None:
    html_text = _render_template(brochure_dir, "back_cover.html.j2", context)
    if html_text is None:
        return None
    css_path = brochure_dir / "style.css"
    stylesheets = [CSS(string=_COVER_BASE_CSS)]
    if css_path.exists():
        stylesheets.append(CSS(filename=str(css_path)))
    HTML(string=html_text, base_url=str(brochure_dir)).write_pdf(str(output_path), stylesheets=stylesheets)
    return output_path
