#!/usr/bin/env python3
"""Fetch an arXiv PDF and summarize it through OpenRouter."""

from __future__ import annotations

import argparse
import base64
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


DEFAULT_MODEL = "google/gemini-3.1-flash-lite-preview"
DEFAULT_ENDPOINT = "https://openrouter.ai/api/v1/chat/completions"


PROMPT = """Create a full outline of this arXiv paper.

Interpret the entire PDF, including the abstract, main text, appendices, figures,
tables, diagrams, screenshots, plots, algorithms, equations, and captions.

Leave citations out:
- Do not include a references or bibliography section.
- Do not list cited works.
- Remove inline citation markers such as [1], [12, 13], or author-year callouts
  unless needed to identify this paper itself.

Use this structure:
1. Title and thesis
2. Problem and motivation
3. Key contributions
4. Method or system design
5. Figures, tables, and visual evidence
6. Experiments or evaluation
7. Results and interpretation
8. Limitations and assumptions
9. Practical implications
10. Reproducibility notes
11. Open questions
"""


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Summarize an arXiv paper PDF with OpenRouter Gemini."
    )
    parser.add_argument("paper", help="arXiv ID, arXiv URL, PDF URL, or local PDF path")
    parser.add_argument("--model", default=os.getenv("OPENROUTER_MODEL", DEFAULT_MODEL))
    parser.add_argument("--endpoint", default=os.getenv("OPENROUTER_ENDPOINT", DEFAULT_ENDPOINT))
    parser.add_argument(
        "--engine",
        default="native",
        choices=("native", "mistral-ocr", "pdf-text"),
        help="OpenRouter PDF parser engine. Use native for visual interpretation.",
    )
    parser.add_argument("--prompt", default=PROMPT)
    parser.add_argument("--save-pdf", help="Optional path to save the fetched PDF")
    parser.add_argument("--json", action="store_true", help="Print full JSON response")
    args = parser.parse_args()

    api_key = os.getenv("OPENROUTER_API_KEY")
    if not api_key:
        print("OPENROUTER_API_KEY is required", file=sys.stderr)
        return 2

    try:
        pdf_bytes, filename, source = load_pdf(args.paper)
    except Exception as exc:
        print(f"failed to load PDF: {exc}", file=sys.stderr)
        return 1

    if args.save_pdf:
        Path(args.save_pdf).write_bytes(pdf_bytes)

    data_url = "data:application/pdf;base64," + base64.b64encode(pdf_bytes).decode("ascii")
    payload = {
        "model": args.model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": args.prompt},
                    {
                        "type": "file",
                        "file": {
                            "filename": filename,
                            "file_data": data_url,
                        },
                    },
                ],
            }
        ],
        "plugins": [
            {
                "id": "file-parser",
                "pdf": {"engine": args.engine},
            }
        ],
        "stream": False,
    }

    request = urllib.request.Request(
        args.endpoint,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "HTTP-Referer": "https://github.com/joshka0/foxctl",
            "X-Title": "foxctl arXiv summarizer",
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            response_data = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        print(f"OpenRouter request failed: HTTP {exc.code}: {detail}", file=sys.stderr)
        return 1
    except Exception as exc:
        print(f"OpenRouter request failed: {exc}", file=sys.stderr)
        return 1

    if args.json:
        print(json.dumps(response_data, indent=2, sort_keys=True))
        return 0

    content = extract_content(response_data)
    if not content:
        print(json.dumps(response_data, indent=2, sort_keys=True))
        return 1

    print(f"Source: {source}", file=sys.stderr)
    print(content.strip())
    return 0


def load_pdf(value: str) -> tuple[bytes, str, str]:
    path = Path(value).expanduser()
    if path.exists():
        if path.suffix.lower() != ".pdf":
            raise ValueError(f"local file is not a PDF: {path}")
        return path.read_bytes(), path.name, str(path)

    url = resolve_arxiv_pdf_url(value)
    request = urllib.request.Request(url, headers={"User-Agent": "foxctl-arxiv-summarizer/1.0"})
    with urllib.request.urlopen(request, timeout=60) as response:
        content_type = response.headers.get("content-type", "")
        data = response.read()

    if b"%PDF" not in data[:1024] and "pdf" not in content_type.lower():
        raise ValueError(f"download did not look like a PDF: {url}")

    filename = pdf_filename_from_url(url)
    return data, filename, url


def resolve_arxiv_pdf_url(value: str) -> str:
    value = value.strip()
    parsed = urllib.parse.urlparse(value)
    if parsed.scheme in {"http", "https"}:
        if "arxiv.org" in parsed.netloc and parsed.path.startswith("/abs/"):
            paper_id = parsed.path.removeprefix("/abs/").strip("/")
            return f"https://arxiv.org/pdf/{paper_id}"
        return value

    paper_id = normalize_arxiv_id(value)
    return f"https://arxiv.org/pdf/{paper_id}"


def normalize_arxiv_id(value: str) -> str:
    value = value.strip()
    value = re.sub(r"^arxiv:", "", value, flags=re.IGNORECASE)
    if not re.match(r"^([a-z-]+(\.[A-Z]{2})?/\d{7}|\d{4}\.\d{4,5})(v\d+)?$", value):
        raise ValueError(f"not an arXiv ID, URL, or local PDF: {value}")
    return value


def pdf_filename_from_url(url: str) -> str:
    parsed = urllib.parse.urlparse(url)
    name = Path(parsed.path).name or "paper.pdf"
    if not name.lower().endswith(".pdf"):
        name += ".pdf"
    return name


def extract_content(response_data: dict) -> str:
    choices = response_data.get("choices") or []
    if not choices:
        return ""
    message = choices[0].get("message") or {}
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for item in content:
            if isinstance(item, dict) and item.get("type") == "text":
                parts.append(str(item.get("text", "")))
        return "\n".join(part for part in parts if part)
    return ""


if __name__ == "__main__":
    raise SystemExit(main())
