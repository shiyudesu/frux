from __future__ import annotations

import re
import unicodedata

from .constants import MAX_DESCRIPTION_CODEPOINTS, MAX_TITLE_CODEPOINTS

WHITESPACE = re.compile(r"\s+", re.UNICODE)


def normalize_field(value: str) -> str:
    normalized = unicodedata.normalize("NFKC", value)
    if any(
        unicodedata.category(char) == "Cs"
        or (unicodedata.category(char) == "Cc" and not char.isspace())
        for char in normalized
    ):
        raise ValueError("invalid text")
    return WHITESPACE.sub(" ", normalized).strip()


def canonical_text(title: str, description: str) -> tuple[str, str, str]:
    title = normalize_field(title)
    description = normalize_field(description)
    if not 1 <= len(title) <= MAX_TITLE_CODEPOINTS:
        raise ValueError("invalid title")
    if len(description) > MAX_DESCRIPTION_CODEPOINTS:
        raise ValueError("invalid description")
    return title, description, title if not description else f"{title}\n{description}"
