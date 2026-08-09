from __future__ import annotations

import json
from pathlib import Path
import sys

from frux_embedding.model import ModelRuntime
from frux_embedding.constants import (
    FIXTURE_ATOL,
    FIXTURE_RTOL,
    MODEL_NAME,
    MODEL_REVISION,
)


model_path = sys.argv[1]
output = Path(sys.argv[2])
runtime = ModelRuntime(model_path, str(output))
runtime.fixture_path = output
from sentence_transformers import SentenceTransformer

runtime.model = SentenceTransformer(model_path, device="cpu", local_files_only=True)
runtime.model.max_seq_length = 128
texts = ["城市夜景\n雨后的街道与霓虹灯", "Frux 短视频\nmultilingual semantic search"]
output.write_text(
    json.dumps(
        {
            "model": MODEL_NAME,
            "revision": MODEL_REVISION,
            "atol": FIXTURE_ATOL,
            "rtol": FIXTURE_RTOL,
            "fixtures": [
                {"text": text, "embedding": runtime.encode([text])[0]}
                for text in texts
            ],
        },
        ensure_ascii=False,
        separators=(",", ":"),
    ),
    encoding="utf-8",
)
