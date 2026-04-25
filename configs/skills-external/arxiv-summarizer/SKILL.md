---
name: arxiv-summarizer
description: "Fetch arXiv PDFs and summarize papers with OpenRouter Gemini, producing a citation-free outline that interprets text, figures, tables, and diagrams."
---

# arXiv Summarizer

Use this skill when the user asks to summarize, outline, review, or explain an arXiv paper from an arXiv URL, paper ID, or local PDF.

## Default Path

Use the bundled helper unless the user explicitly asks for a different workflow:

```bash
python3 configs/skills-external/arxiv-summarizer/scripts/summarize_arxiv.py <arxiv-id-or-url-or-pdf>
```

Requirements:

- `OPENROUTER_API_KEY` must be set.
- Default model: `google/gemini-3.1-flash-lite-preview`.
- Default OpenRouter endpoint: `https://openrouter.ai/api/v1/chat/completions`.

The helper:

1. Resolves arXiv IDs and `/abs/` URLs to the matching `/pdf/` URL.
2. Downloads remote PDFs, or reads a local `.pdf`.
3. Sends the PDF as a base64 `file` part to OpenRouter.
4. Requests a full outline of the paper.
5. Explicitly asks the model to interpret figures, tables, diagrams, screenshots, and other visual material.
6. Excludes citations, bibliography, and inline citation callouts from the final summary.

## Output Contract

Return the model's outline directly. Prefer this structure:

- Title and one-paragraph thesis.
- Problem and motivation.
- Key contributions.
- Method or system design.
- Figures, tables, and visual evidence.
- Experiments or evaluation.
- Results and interpretation.
- Limitations and assumptions.
- Practical implications.
- Reproducibility notes.
- Open questions.

Do not include a references section. Do not list cited works. Avoid inline citation markers such as `[12]`, `(Smith et al.)`, or bibliography-style details unless they are necessary to identify the paper itself.

## Manual API Shape

If the helper is unsuitable, send a Chat Completions request with a PDF file content part:

```json
{
  "model": "google/gemini-3.1-flash-lite-preview",
  "messages": [
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "Create a full citation-free outline of this paper. Interpret all figures, tables, diagrams, and visual content."
        },
        {
          "type": "file",
          "file": {
            "filename": "paper.pdf",
            "file_data": "data:application/pdf;base64,<base64>"
          }
        }
      ]
    }
  ],
  "plugins": [
    {
      "id": "file-parser",
      "pdf": {
        "engine": "native"
      }
    }
  ]
}
```

Use `engine: "native"` by default so Gemini can inspect the PDF visually. If OpenRouter rejects native processing for a specific model or file, retry with `mistral-ocr`.
