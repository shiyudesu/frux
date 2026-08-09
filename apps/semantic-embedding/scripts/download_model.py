from __future__ import annotations

import json
from pathlib import Path
import sys

from huggingface_hub import snapshot_download

from frux_embedding.constants import MODEL_NAME, MODEL_REVISION


target = Path(sys.argv[1])
snapshot_download(
    repo_id=MODEL_NAME,
    revision=MODEL_REVISION,
    local_dir=target,
    allow_patterns=[
        "1_Pooling/config.json",
        "config.json",
        "config_sentence_transformers.json",
        "model.safetensors",
        "modules.json",
        "sentence_bert_config.json",
        "sentencepiece.bpe.model",
        "special_tokens_map.json",
        "tokenizer.json",
        "tokenizer_config.json",
    ],
)
(target / "frux-model.json").write_text(
    json.dumps({"model": MODEL_NAME, "revision": MODEL_REVISION}),
    encoding="utf-8",
)
