from __future__ import annotations

import json
import math
from pathlib import Path
import random
from typing import Protocol

import numpy as np

from .constants import (
    CHUNK_SIZE,
    FIXTURE_ATOL,
    FIXTURE_RTOL,
    MAX_SEQUENCE_TOKENS,
    MODEL_DIMENSION,
    MODEL_NAME,
    MODEL_REVISION,
)


class Encoder(Protocol):
    def encode(self, texts: list[str]) -> np.ndarray: ...


class ModelRuntime:
    def __init__(self, model_path: str, fixture_path: str) -> None:
        self.model_path = Path(model_path)
        self.fixture_path = Path(fixture_path)
        self.model = None
        self.ready = False

    def load(self) -> None:
        import torch
        from sentence_transformers import SentenceTransformer

        random.seed(0)
        np.random.seed(0)
        torch.manual_seed(0)
        torch.set_num_threads(2)
        torch.set_num_interop_threads(2)
        metadata = json.loads((self.model_path / "frux-model.json").read_text())
        if metadata != {"model": MODEL_NAME, "revision": MODEL_REVISION}:
            raise RuntimeError("model metadata mismatch")
        model = SentenceTransformer(
            str(self.model_path), device="cpu", local_files_only=True
        )
        model.max_seq_length = MAX_SEQUENCE_TOKENS
        model.eval()
        self.model = model
        fixture_contract = json.loads(self.fixture_path.read_text())
        if (
            fixture_contract.get("model") != MODEL_NAME
            or fixture_contract.get("revision") != MODEL_REVISION
            or fixture_contract.get("atol") != FIXTURE_ATOL
            or fixture_contract.get("rtol") != FIXTURE_RTOL
            or len(fixture_contract.get("fixtures", [])) != 2
        ):
            raise RuntimeError("fixture metadata mismatch")
        for fixture in fixture_contract["fixtures"]:
            actual = self.encode([fixture["text"]])[0]
            expected = np.asarray(fixture["embedding"], dtype=np.float32)
            if expected.shape != (MODEL_DIMENSION,) or not np.allclose(
                actual, expected, atol=FIXTURE_ATOL, rtol=FIXTURE_RTOL
            ):
                raise RuntimeError("model fixture mismatch")
        self.ready = True

    def encode(self, texts: list[str]) -> list[list[float]]:
        import torch

        if self.model is None:
            raise RuntimeError("model unavailable")
        outputs: list[list[float]] = []
        for start in range(0, len(texts), CHUNK_SIZE):
            chunk = texts[start : start + CHUNK_SIZE]
            with torch.inference_mode():
                values = np.asarray(
                    self.model.encode(
                        chunk,
                        batch_size=CHUNK_SIZE,
                        convert_to_numpy=True,
                        normalize_embeddings=True,
                        show_progress_bar=False,
                    ),
                    dtype=np.float32,
                )
            if values.shape != (len(chunk), MODEL_DIMENSION) or not np.isfinite(values).all():
                raise RuntimeError("invalid model output")
            norms = np.linalg.norm(values, axis=1)
            if not np.allclose(norms, 1.0, atol=1e-4, rtol=0):
                raise RuntimeError("invalid model output")
            outputs.extend(values.tolist())
        return outputs
