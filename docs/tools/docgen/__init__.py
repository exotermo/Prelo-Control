"""docgen: normalizes a Markdown document (with Mermaid diagrams) into a PDF,
with an optional swappable cover ("brochure") prepended.

Each concern is its own module so a piece can be reused or replaced on its own:
- mermaid.py    renders ```mermaid fences to images
- markdown_render.py  converts the (mermaid-substituted) Markdown to styled HTML, then to a PDF
- brochure.py   renders a Jinja2-templated cover page from a "brochure directory"
- pdf.py        merges cover + body PDFs
- cli.py        argument parsing and orchestration
"""
