"""Extracts ```mermaid fenced blocks from Markdown and renders each to an SVG,
via the mermaid-cli (`mmdc`), run through `npx` so no global install is required.
"""
from __future__ import annotations

import re
import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path

_FENCE_RE = re.compile(r"```mermaid\n(?P<body>.*?)\n```", re.DOTALL)


@dataclass(frozen=True)
class MermaidBlock:
    index: int
    source: str
    image_path: Path


class MermaidRenderError(RuntimeError):
    pass


def find_blocks(markdown_text: str) -> list[str]:
    return [m.group("body") for m in _FENCE_RE.finditer(markdown_text)]


def render_block(source: str, index: int, work_dir: Path, mmdc_cmd: list[str]) -> Path:
    """Renders one Mermaid diagram source to a PNG file under work_dir and returns its path.

    PNG (not SVG) is used deliberately: mermaid-cli renders node labels as HTML
    (<foreignObject>) inside the SVG, which WeasyPrint's SVG engine does not paint when the
    SVG is later embedded as an <img> — labels would render as empty boxes. PNG is a full
    browser screenshot (via Puppeteer) and always has the text baked in.
    """
    input_path = work_dir / f"diagram-{index}.mmd"
    output_path = work_dir / f"diagram-{index}.png"
    input_path.write_text(source, encoding="utf-8")
    cmd = [*mmdc_cmd, "-i", str(input_path), "-o", str(output_path), "-b", "white", "-s", "2"]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0 or not output_path.exists():
        raise MermaidRenderError(
            f"mermaid-cli failed for diagram #{index}:\n{result.stdout}\n{result.stderr}"
        )
    return output_path


def default_mmdc_command() -> list[str]:
    """Prefers a locally installed `mmdc`; falls back to a one-off `npx` invocation."""
    mmdc = shutil.which("mmdc")
    if mmdc:
        return [mmdc]
    if shutil.which("npx") is None:
        raise MermaidRenderError(
            "neither `mmdc` nor `npx` is available on PATH; install @mermaid-js/mermaid-cli "
            "or pass --no-mermaid to skip diagram rendering"
        )
    return ["npx", "--yes", "-p", "@mermaid-js/mermaid-cli", "mmdc"]


def render_all(markdown_text: str, work_dir: Path, mmdc_cmd: list[str] | None = None) -> tuple[str, list[MermaidBlock]]:
    """Replaces every ```mermaid fence with a Markdown image reference to its rendered SVG.

    Returns the rewritten Markdown text and the list of rendered blocks (for cleanup/inspection).
    """
    mmdc_cmd = mmdc_cmd or default_mmdc_command()
    blocks: list[MermaidBlock] = []

    def _replace(match: re.Match) -> str:
        index = len(blocks)
        source = match.group("body")
        image_path = render_block(source, index, work_dir, mmdc_cmd)
        blocks.append(MermaidBlock(index=index, source=source, image_path=image_path))
        return f"![Diagrama {index + 1}]({image_path.as_posix()})"

    rewritten = _FENCE_RE.sub(_replace, markdown_text)
    return rewritten, blocks
