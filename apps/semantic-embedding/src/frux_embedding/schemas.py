from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

from .constants import (
    MAX_BATCH_SIZE,
    MAX_DESCRIPTION_CODEPOINTS,
    MAX_REQUEST_BYTES,
    MAX_TITLE_CODEPOINTS,
    MAX_TOTAL_CODEPOINTS,
)


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class EmbeddingItemRequest(StrictModel):
    id: str
    title: str
    description: str


class EmbeddingRequest(StrictModel):
    items: list[EmbeddingItemRequest] = Field(min_length=1, max_length=MAX_BATCH_SIZE)


class Limits(StrictModel):
    max_batch_size: int = MAX_BATCH_SIZE
    max_title_codepoints: int = MAX_TITLE_CODEPOINTS
    max_description_codepoints: int = MAX_DESCRIPTION_CODEPOINTS
    max_total_codepoints: int = MAX_TOTAL_CODEPOINTS
    max_request_bytes: int = MAX_REQUEST_BYTES


class ModelMetadata(StrictModel):
    model: str
    revision: str
    dimension: int
    max_sequence_tokens: int
    dtype: str
    normalized: bool
    device: str
    limits: Limits


class EmbeddingItemResponse(StrictModel):
    id: str
    index: int
    embedding: list[float]


class EmbeddingResponse(StrictModel):
    model: str
    revision: str
    dimension: int
    items: list[EmbeddingItemResponse]
