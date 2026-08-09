FROM ghcr.io/astral-sh/uv:0.8.3@sha256:88baae1f9fa298996f8313e44559163c535937406d217f1c8ac9d4b86a2020fd AS uv
FROM python:3.12.11-slim-bookworm@sha256:c00fc7b44d844b6da22861ec24af43968a5200eac4ec607b4725d585165d6b49 AS builder

COPY --from=uv /uv /usr/local/bin/uv
WORKDIR /build
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev --no-install-project
COPY src ./src
COPY scripts ./scripts
RUN uv sync --frozen --no-dev --no-editable && \
    .venv/bin/python scripts/download_model.py /models/paraphrase-multilingual-MiniLM-L12-v2

FROM python:3.12.11-slim-bookworm@sha256:c00fc7b44d844b6da22861ec24af43968a5200eac4ec607b4725d585165d6b49
ENV PATH=/app/.venv/bin:$PATH \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    TOKENIZERS_PARALLELISM=false \
    OMP_NUM_THREADS=2 \
    MKL_NUM_THREADS=2 \
    OPENBLAS_NUM_THREADS=2 \
    NUMEXPR_NUM_THREADS=2 \
    TMPDIR=/run/frux-tmp
WORKDIR /app
COPY --from=builder /build/.venv /app/.venv
COPY --from=builder /build/src /app/src
COPY --from=builder /models /opt/frux/models
COPY fixtures /app/fixtures
COPY image_contract.py /app/image_contract.py
RUN groupadd --gid 10001 frux && useradd --uid 10001 --gid 10001 --no-create-home frux && \
    mkdir -p /run/frux-tmp && chown 10001:10001 /run/frux-tmp && \
    chmod -R a-w /opt/frux/models /app
USER 10001:10001
EXPOSE 8081
CMD ["python", "-m", "frux_embedding.main"]
