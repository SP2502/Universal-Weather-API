#!/usr/bin/env python3
"""Guardrail for a deterministic UWA runtime; no LLM SDKs or model endpoints.

This is a dependency/configuration policy check, not a security scanner or a
claim that arbitrary code cannot make network requests to an AI API.
"""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parent.parent
# Match recognizable actual external AI model dependencies / runtime endpoints.
FORBIDDEN = re.compile(
    r"(?i)(?:github\.com/(?:openai|anthropics|sashabaranov/go-openai|google/generative-ai-go)|"
    r"generativelanguage\.googleapis\.com|api\.openai\.com|api\.anthropic\.com|"
    r"\b(?:OPENAI_API_KEY|ANTHROPIC_API_KEY|GEMINI_API_KEY|LLM_API_KEY)\b)"
)
checks = [ROOT / "go.mod", *ROOT.glob("cmd/**/*.go"), *ROOT.glob("internal/**/*.go"),
          *ROOT.glob("providers/**/*.go"), *ROOT.glob("schemas/**/*.go")]
failures = []
for path in checks:
    if not path.is_file():
        continue
    content = path.read_text(encoding="utf-8", errors="replace")
    for number, line in enumerate(content.splitlines(), 1):
        if FORBIDDEN.search(line):
            failures.append(f"{path.relative_to(ROOT)}:{number}: disallowed model SDK, endpoint or configuration")
if failures:
    print("\n".join(failures), file=sys.stderr)
    sys.exit(1)
print(f"Rule-based-only dependency check passed ({len(checks)} source/module files checked).")
