#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=prod-deploy.sh
source "$SCRIPT_DIR/prod-deploy.sh"

test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
FRUX_ROOT=$test_root

write_valid_multimodal_env() {
  printf '%s\n' \
    'FRUX_HMAC_SECRET=CI-Only-Application-HMAC-Secret-123!' \
    'FRUX_MULTIMODAL_DEPLOYMENT_ENABLED=true' \
    'FRUX_MULTIMODAL_PROFILE=tongyi-embedding-vision-flash-2026-03-06' \
    'FRUX_MULTIMODAL_ENDPOINT=http://multimodal-provider:8099' \
    'FRUX_MULTIMODAL_HMAC_SECRET=CI-Only-Multimodal-HMAC-Secret-123!' \
    'FRUX_MULTIMODAL_ENABLED=true' \
    'FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED=true' \
    'FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED=true' \
    'FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED=false' \
    'FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED=true' \
    'FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK=true' \
    'DASHSCOPE_API_KEY=ci-dashscope-key' \
    'FRUX_TONGYI_UPSTREAM_TIMEOUT=20s' \
    'FRUX_TONGYI_MAX_REQUEST_BYTES=25165824' \
    'FRUX_TONGYI_MAX_RESPONSE_BYTES=2097152' \
    'FRUX_TONGYI_SHUTDOWN_TIMEOUT=10s' >"$FRUX_ROOT/.env.prod"
}

replace_env_value() {
  local name=$1
  local value=$2

  sed -i "s|^${name}=.*$|${name}=${value}|" "$FRUX_ROOT/.env.prod"
}

expect_validation_failure() {
  local description=$1

  if (validate_multimodal_deployment_config >/dev/null 2>&1); then
    printf 'Expected multimodal deployment validation failure: %s\n' "$description" >&2
    exit 1
  fi
}

write_valid_multimodal_env
validate_multimodal_deployment_config
multimodal_deployment_enabled

FRUX_DEPLOY_MULTIMODAL_OVERRIDE=false
if multimodal_deployment_enabled; then
  echo 'Explicit rollback override did not disable the multimodal profile' >&2
  exit 1
fi
unset FRUX_DEPLOY_MULTIMODAL_OVERRIDE

replace_env_value FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED false
expect_validation_failure 'partial production feature set'

write_valid_multimodal_env
replace_env_value FRUX_MULTIMODAL_ENDPOINT http://adapter.internal:8099
expect_validation_failure 'arbitrary private HTTP endpoint'

write_valid_multimodal_env
replace_env_value FRUX_MULTIMODAL_ENABLED yes
expect_validation_failure 'non-boolean feature flag'

write_valid_multimodal_env
replace_env_value FRUX_MULTIMODAL_HMAC_SECRET CI-Only-Application-HMAC-Secret-123!
expect_validation_failure 'shared application and provider HMAC'

write_valid_multimodal_env
replace_env_value FRUX_TONGYI_MAX_REQUEST_BYTES 9999999999
expect_validation_failure 'oversized Adapter request bound'

write_valid_multimodal_env
replace_env_value FRUX_MULTIMODAL_DEPLOYMENT_ENABLED false
replace_env_value FRUX_MULTIMODAL_ENABLED false
replace_env_value FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED false
replace_env_value FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED false
replace_env_value FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED false
replace_env_value FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK false
validate_multimodal_deployment_config

mkdir -p "$FRUX_ROOT/old/apps" "$FRUX_ROOT/new/apps"
printf '%s\n' 'services:' '  api:' '    image: example.invalid/api' >"$FRUX_ROOT/old/apps/docker-compose.prod.yml"
printf '%s\n' 'services:' '  multimodal-provider:' '    image: example.invalid/api' >"$FRUX_ROOT/new/apps/docker-compose.prod.yml"
if release_supports_multimodal "$FRUX_ROOT/old"; then
  echo 'Legacy release was incorrectly detected as multimodal-capable' >&2
  exit 1
fi
release_supports_multimodal "$FRUX_ROOT/new"

write_valid_multimodal_env
if (validate_release_multimodal_support "$FRUX_ROOT/old" >/dev/null 2>&1); then
  echo 'Legacy release accepted an enabled multimodal deployment' >&2
  exit 1
fi
validate_release_multimodal_support "$FRUX_ROOT/new"

echo 'Prod deployment multimodal validation tests passed.'
