#!/usr/bin/env bash

set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
project="frux-prod-minio-check-$$"
object_names=(
  uploads/compose-check.txt
  processed/compose-check.txt
  moderation/compose-check.txt
  media/compose-check.txt
)
compose=(
  docker compose
  --env-file "$root/apps/.env.release.example"
  -p "$project"
  -f "$root/apps/docker-compose.prod.yml"
)

cleanup() {
  "${compose[@]}" down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

for name in \
  FRUX_DOMAIN \
  FRUX_PUBLIC_HTTPS_PORT \
  FRUX_S3_DOMAIN \
  FRUX_MINIO_ROOT_USER \
  FRUX_MINIO_ROOT_PASSWORD \
  FRUX_S3_ACCESS_KEY \
  FRUX_S3_SECRET_KEY \
  FRUX_S3_BUCKET; do
  [[ -n ${!name:-} ]] || {
    printf 'missing required environment variable: %s\n' "$name" >&2
    exit 1
  }
done

"${compose[@]}" up -d minio
"${compose[@]}" run --rm minio-init
"${compose[@]}" run --rm minio-init

"${compose[@]}" run --rm --no-deps --entrypoint /bin/sh minio-init -ec '
  printf "frux-minio-check" >/tmp/check.txt
  mc alias set app http://minio:9000 "$FRUX_S3_ACCESS_KEY" "$FRUX_S3_SECRET_KEY"
  for key in \
    uploads/compose-check.txt \
    processed/compose-check.txt \
    moderation/compose-check.txt \
    media/compose-check.txt; do
    mc cp /tmp/check.txt "app/$FRUX_S3_BUCKET/$key" >/dev/null
    mc stat "app/$FRUX_S3_BUCKET/$key" >/dev/null
  done
'

if "${compose[@]}" run --rm --no-deps --entrypoint /bin/sh minio-init -ec '
  mc alias set app http://minio:9000 "$FRUX_S3_ACCESS_KEY" "$FRUX_S3_SECRET_KEY"
  mc anonymous set download "app/$FRUX_S3_BUCKET"
' >/dev/null 2>&1; then
  echo "Frux application credentials can modify anonymous Bucket access" >&2
  exit 1
fi

if "${compose[@]}" run --rm --no-deps --entrypoint /bin/sh minio-init -ec '
  mc alias set app http://minio:9000 "$FRUX_S3_ACCESS_KEY" "$FRUX_S3_SECRET_KEY"
  mc cat "app/$FRUX_S3_BUCKET/.frux/application-access-key"
' >/dev/null 2>&1; then
  echo "Frux application credentials can read the root-managed identity marker" >&2
  exit 1
fi

for object_name in "${object_names[@]}"; do
  anonymous_status=$(
    curl \
      --silent \
      --show-error \
      --output /dev/null \
      --write-out '%{http_code}' \
      "http://127.0.0.1:${FRUX_MINIO_API_PORT:-19000}/${FRUX_S3_BUCKET}/${object_name}"
  )
  [[ $anonymous_status == 403 ]] || {
    printf 'anonymous object request for %s returned %s, want 403\n' \
      "$object_name" "$anonymous_status" >&2
    exit 1
  }
done

allowed_origin="https://${FRUX_DOMAIN}:${FRUX_PUBLIC_HTTPS_PORT}"
allowed_headers=$(
  curl \
    --silent \
    --show-error \
    --dump-header - \
    --output /dev/null \
    --request OPTIONS \
    "http://127.0.0.1:${FRUX_MINIO_API_PORT:-19000}/${FRUX_S3_BUCKET}/${object_names[0]}" \
    --header "Origin: ${allowed_origin}" \
    --header 'Access-Control-Request-Method: PUT' \
    --header 'Access-Control-Request-Headers: content-type,cache-control,x-amz-checksum-sha256,x-amz-meta-sha256' |
    tr -d '\r'
)
grep -Fqx "Access-Control-Allow-Origin: ${allowed_origin}" <<<"$allowed_headers"
grep -Fqx 'Access-Control-Allow-Methods: PUT' <<<"$allowed_headers"

denied_headers=$(
  curl \
    --silent \
    --show-error \
    --dump-header - \
    --output /dev/null \
    --request OPTIONS \
    "http://127.0.0.1:${FRUX_MINIO_API_PORT:-19000}/${FRUX_S3_BUCKET}/${object_names[0]}" \
    --header 'Origin: https://untrusted.example.com:18443' \
    --header 'Access-Control-Request-Method: PUT' |
    tr -d '\r'
)
if grep -q '^Access-Control-Allow-Origin:' <<<"$denied_headers"; then
  echo "untrusted origin received MinIO CORS permission" >&2
  exit 1
fi

"${compose[@]}" restart minio >/dev/null
for _ in $(seq 1 30); do
  if curl \
    --fail \
    --silent \
    --show-error \
    "http://127.0.0.1:${FRUX_MINIO_API_PORT:-19000}/minio/health/live" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl \
  --fail \
  --silent \
  --show-error \
  "http://127.0.0.1:${FRUX_MINIO_API_PORT:-19000}/minio/health/live" >/dev/null

"${compose[@]}" run --rm --no-deps --entrypoint /bin/sh minio-init -ec '
  mc alias set app http://minio:9000 "$FRUX_S3_ACCESS_KEY" "$FRUX_S3_SECRET_KEY"
  for key in \
    uploads/compose-check.txt \
    processed/compose-check.txt \
    moderation/compose-check.txt \
    media/compose-check.txt; do
    mc stat "app/$FRUX_S3_BUCKET/$key" >/dev/null
  done
'

rotated_access_key="${FRUX_S3_ACCESS_KEY}-rotated"
rotated_secret_key="${FRUX_S3_SECRET_KEY}-rotated"
"${compose[@]}" run --rm \
  -e "FRUX_S3_ACCESS_KEY=${rotated_access_key}" \
  -e "FRUX_S3_SECRET_KEY=${rotated_secret_key}" \
  minio-init

if "${compose[@]}" run --rm --no-deps --entrypoint /bin/sh minio-init -ec '
  mc alias set old http://minio:9000 "$FRUX_S3_ACCESS_KEY" "$FRUX_S3_SECRET_KEY"
  mc stat "old/$FRUX_S3_BUCKET/uploads/compose-check.txt"
' >/dev/null 2>&1; then
  echo "rotated MinIO application credentials remain active" >&2
  exit 1
fi

"${compose[@]}" run --rm --no-deps \
  -e "FRUX_S3_ACCESS_KEY=${rotated_access_key}" \
  -e "FRUX_S3_SECRET_KEY=${rotated_secret_key}" \
  --entrypoint /bin/sh minio-init -ec '
    mc alias set rotated http://minio:9000 "$FRUX_S3_ACCESS_KEY" "$FRUX_S3_SECRET_KEY"
    mc stat "rotated/$FRUX_S3_BUCKET/uploads/compose-check.txt" >/dev/null
  '
