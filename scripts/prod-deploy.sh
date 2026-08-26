#!/usr/bin/env bash

set -Eeuo pipefail
set +x
umask 077

FRUX_ROOT=${FRUX_ROOT:-/opt/frux}
FRUX_DEPLOY_IMAGE=${FRUX_DEPLOY_IMAGE:-ghcr.io/shiyudesu/frux-deploy:prod}
DOCKER_BIN=${DOCKER_BIN:-docker}
FLOCK_BIN=${FLOCK_BIN:-flock}
SLEEP_BIN=${SLEEP_BIN:-sleep}
CURL_BIN=${CURL_BIN:-curl}
FRUX_HEALTH_ATTEMPTS=${FRUX_HEALTH_ATTEMPTS:-36}
FRUX_HEALTH_SLEEP=${FRUX_HEALTH_SLEEP:-5}

die() {
  printf 'Frux deployment failed: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command is unavailable: $1"
}

safe_release_path() {
  local path=$1
  [[ $path == "$FRUX_ROOT/releases/"* && $path != "$FRUX_ROOT/releases/" ]]
}

compose_release() {
  local release=$1
  local -a compose_args
  shift

  compose_args=(
    --progress plain
    --env-file "$FRUX_ROOT/.env.prod"
    --env-file "$release/apps/.env.release"
    -p frux-prod
    -f "$release/apps/docker-compose.prod.yml"
  )
  if multimodal_deployment_enabled; then
    compose_args+=(--profile multimodal)
  fi
  "$DOCKER_BIN" compose "${compose_args[@]}" "$@"
}

wait_healthy() {
  local release=$1
  local service=$2
  local container status
  local attempt

  for ((attempt = 1; attempt <= FRUX_HEALTH_ATTEMPTS; attempt++)); do
    container=$(compose_release "$release" ps -q "$service" 2>/dev/null || true)
    if [[ -n $container ]]; then
      status=$(
        "$DOCKER_BIN" inspect \
          -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
          "$container" 2>/dev/null || true
      )
      [[ $status == healthy ]] && return 0
      [[ $status == unhealthy || $status == exited || $status == dead ]] && return 1
    fi
    "$SLEEP_BIN" "$FRUX_HEALTH_SLEEP"
  done
  return 1
}

prod_env_value() {
  local name=$1
  sed -n "s/^${name}=//p" "$FRUX_ROOT/.env.prod" | tail -n 1
}

prod_env_value_or() {
  local name=$1
  local fallback=$2
  local value

  value=$(prod_env_value "$name")
  printf '%s' "${value:-$fallback}"
}

multimodal_deployment_enabled() {
  case "${FRUX_DEPLOY_MULTIMODAL_OVERRIDE:-}" in
    true) return 0 ;;
    false) return 1 ;;
    "") ;;
    *) die "FRUX_DEPLOY_MULTIMODAL_OVERRIDE must be true or false" ;;
  esac
  [[ $(prod_env_value_or FRUX_MULTIMODAL_DEPLOYMENT_ENABLED false) == true ]]
}

validate_multimodal_deployment_config() {
  local enabled profile endpoint hmac api_key application_hmac
  local runtime video_jobs query_embedding hybrid_search session production_full development_full private_http
  local hybrid_min_similarity hybrid_max_semantic_only
  local upstream_timeout shutdown_timeout max_request_bytes max_response_bytes
  local name value

  enabled=$(prod_env_value_or FRUX_MULTIMODAL_DEPLOYMENT_ENABLED false)
  [[ $enabled == true || $enabled == false ]] ||
    die "FRUX_MULTIMODAL_DEPLOYMENT_ENABLED must be true or false"
  runtime=$(prod_env_value_or FRUX_MULTIMODAL_ENABLED false)
  video_jobs=$(prod_env_value_or FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED false)
  query_embedding=$(prod_env_value_or FRUX_MULTIMODAL_QUERY_EMBEDDING_ENABLED false)
  hybrid_search=$(prod_env_value_or FRUX_MULTIMODAL_HYBRID_SEARCH_ENABLED false)
  session=$(prod_env_value_or FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED false)
  production_full=$(prod_env_value_or FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED false)
  development_full=$(prod_env_value_or FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED false)
  private_http=$(prod_env_value_or FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK false)
  hybrid_min_similarity=$(prod_env_value_or FRUX_MULTIMODAL_HYBRID_MIN_SIMILARITY 0.55)
  hybrid_max_semantic_only=$(prod_env_value_or FRUX_MULTIMODAL_HYBRID_MAX_SEMANTIC_ONLY 5)
  for name in \
    FRUX_MULTIMODAL_ENABLED \
    FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED \
    FRUX_MULTIMODAL_QUERY_EMBEDDING_ENABLED \
    FRUX_MULTIMODAL_HYBRID_SEARCH_ENABLED \
    FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED \
    FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED \
    FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED \
    FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK; do
    value=$(prod_env_value_or "$name" false)
    [[ $value == true || $value == false ]] || die "$name must be true or false"
  done

  if [[ $enabled == false ]]; then
    [[ $runtime != true && $video_jobs != true && $query_embedding != true &&
      $hybrid_search != true && $session != true &&
      $production_full != true && $development_full != true && $private_http != true ]] ||
      die "multimodal feature flags require FRUX_MULTIMODAL_DEPLOYMENT_ENABLED=true"
    return 0
  fi

  profile=$(prod_env_value FRUX_MULTIMODAL_PROFILE)
  endpoint=$(prod_env_value FRUX_MULTIMODAL_ENDPOINT)
  hmac=$(prod_env_value FRUX_MULTIMODAL_HMAC_SECRET)
  api_key=$(prod_env_value DASHSCOPE_API_KEY)
  application_hmac=$(prod_env_value FRUX_HMAC_SECRET)
  upstream_timeout=$(prod_env_value_or FRUX_TONGYI_UPSTREAM_TIMEOUT 20s)
  shutdown_timeout=$(prod_env_value_or FRUX_TONGYI_SHUTDOWN_TIMEOUT 10s)
  max_request_bytes=$(prod_env_value_or FRUX_TONGYI_MAX_REQUEST_BYTES 25165824)
  max_response_bytes=$(prod_env_value_or FRUX_TONGYI_MAX_RESPONSE_BYTES 2097152)
  case "$profile" in
    tongyi-embedding-vision-flash-2026-03-06|tongyi-embedding-vision-flash) ;;
    *) die "FRUX_MULTIMODAL_PROFILE is not a registered production profile" ;;
  esac
  [[ $endpoint == http://multimodal-provider:8099 ]] ||
    die "FRUX_MULTIMODAL_ENDPOINT must use the private multimodal-provider service"
  [[ ${#hmac} -ge 32 && ${#hmac} -le 512 ]] ||
    die "FRUX_MULTIMODAL_HMAC_SECRET must contain between 32 and 512 characters"
  [[ -n $api_key ]] || die "DASHSCOPE_API_KEY is required for multimodal deployment"
  [[ $hmac != "$application_hmac" ]] ||
    die "FRUX_MULTIMODAL_HMAC_SECRET must differ from FRUX_HMAC_SECRET"
  [[ $upstream_timeout =~ ^[1-9][0-9]*(ms|s|m)$ ]] ||
    die "FRUX_TONGYI_UPSTREAM_TIMEOUT must be a positive simple duration"
  [[ $shutdown_timeout =~ ^[1-9][0-9]*(ms|s|m)$ ]] ||
    die "FRUX_TONGYI_SHUTDOWN_TIMEOUT must be a positive simple duration"
  [[ $max_request_bytes =~ ^[1-9][0-9]{0,9}$ ]] ||
    die "FRUX_TONGYI_MAX_REQUEST_BYTES must be a positive integer"
  [[ $max_response_bytes =~ ^[1-9][0-9]{0,9}$ ]] ||
    die "FRUX_TONGYI_MAX_RESPONSE_BYTES must be a positive integer"
  ((max_request_bytes <= 64 * 1024 * 1024)) ||
    die "FRUX_TONGYI_MAX_REQUEST_BYTES exceeds the 64 MiB deployment limit"
  ((max_response_bytes <= max_request_bytes)) ||
    die "FRUX_TONGYI_MAX_RESPONSE_BYTES must not exceed the request bound"
  [[ $runtime == true && $video_jobs == true && $session == true &&
    $production_full == true && $private_http == true ]] ||
    die "production multimodal runtime, video jobs, Session, full rollout, and private HTTP must be enabled together"
  [[ $hybrid_search != true || $query_embedding == true ]] ||
    die "multimodal hybrid search requires query embedding"
  [[ $development_full != true ]] ||
    die "development full rollout must remain disabled in production"
  awk -v value="$hybrid_min_similarity" 'BEGIN { exit !(value > 0 && value < 1) }' ||
    die "FRUX_MULTIMODAL_HYBRID_MIN_SIMILARITY must be between 0 and 1"
  [[ $hybrid_max_semantic_only =~ ^[1-9][0-9]*$ ]] &&
    ((hybrid_max_semantic_only <= 20)) ||
    die "FRUX_MULTIMODAL_HYBRID_MAX_SEMANTIC_ONLY must be between 1 and 20"
}

release_supports_multimodal() {
  local release=$1

  grep -Eq '^[[:space:]]{2}multimodal-provider:' "$release/apps/docker-compose.prod.yml"
}

validate_release_multimodal_support() {
  local release=$1

  if multimodal_deployment_enabled && ! release_supports_multimodal "$release"; then
    die "approved deployment bundle does not support the multimodal profile; deploy it with multimodal disabled first"
  fi
}

valid_port() {
  local value=$1
  [[ $value =~ ^[1-9][0-9]{0,4}$ ]] && ((value <= 65535))
}

valid_ipv4() {
  local value=$1
  local octets octet

  IFS=. read -r -a octets <<<"$value"
  [[ ${#octets[@]} -eq 4 ]] || return 1
  for octet in "${octets[@]}"; do
    [[ $octet =~ ^(0|[1-9][0-9]{0,2})$ ]] || return 1
    ((10#$octet <= 255)) || return 1
  done
}

prod_hostname() {
  local name=$1
  local value

  value=$(prod_env_value "$name")
  [[ $value =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] &&
    [[ $value == *.* ]] &&
    [[ $value != *..* ]] ||
    die "$name in .env.prod must be an unquoted hostname"
  printf '%s' "$value"
}

public_app_port() {
  local legacy

  legacy=$(prod_env_value FRUX_PUBLIC_HTTPS_PORT)
  prod_env_value_or FRUX_PUBLIC_APP_PORT "$legacy"
}

public_s3_port() {
  local legacy

  legacy=$(prod_env_value FRUX_PUBLIC_HTTPS_PORT)
  prod_env_value_or FRUX_PUBLIC_S3_PORT "$legacy"
}

validate_public_access_config() {
  local mode scheme app_host s3_host app_port s3_port bind_address require_https
  local web_port minio_port

  mode=$(prod_env_value_or FRUX_PUBLIC_ACCESS_MODE caddy-https)
  scheme=$(prod_env_value_or FRUX_PUBLIC_SCHEME https)
  app_host=$(prod_env_value FRUX_DOMAIN)
  s3_host=$(prod_env_value FRUX_S3_DOMAIN)
  app_port=$(public_app_port)
  s3_port=$(public_s3_port)
  bind_address=$(prod_env_value_or FRUX_PUBLIC_BIND_ADDRESS 127.0.0.1)
  require_https=$(prod_env_value_or FRUX_S3_REQUIRE_PUBLIC_HTTPS true)

  valid_port "$app_port" || die "public application port in .env.prod is invalid"
  valid_port "$s3_port" || die "public S3 port in .env.prod is invalid"

  case "$mode" in
    caddy-https)
      prod_hostname FRUX_DOMAIN >/dev/null
      prod_hostname FRUX_S3_DOMAIN >/dev/null
      [[ $app_host != "$s3_host" ]] ||
        die "caddy-https requires distinct FRUX_DOMAIN and FRUX_S3_DOMAIN values"
      [[ $scheme == https ]] || die "caddy-https requires FRUX_PUBLIC_SCHEME=https"
      [[ $app_port == "$s3_port" ]] ||
        die "caddy-https requires the application and S3 public ports to match"
      [[ $bind_address == 127.0.0.1 ]] ||
        die "caddy-https requires FRUX_PUBLIC_BIND_ADDRESS=127.0.0.1"
      [[ $require_https == true ]] ||
        die "caddy-https requires FRUX_S3_REQUIRE_PUBLIC_HTTPS=true"
      ;;
    direct-http)
      valid_ipv4 "$app_host" || die "direct-http requires FRUX_DOMAIN to be an IPv4 literal"
      [[ $app_host == "$s3_host" ]] ||
        die "direct-http requires FRUX_DOMAIN and FRUX_S3_DOMAIN to use the same IPv4 literal"
      [[ $scheme == http ]] || die "direct-http requires FRUX_PUBLIC_SCHEME=http"
      [[ $app_port != "$s3_port" ]] ||
        die "direct-http requires distinct application and S3 public ports"
      [[ $bind_address == 0.0.0.0 ]] ||
        die "direct-http requires FRUX_PUBLIC_BIND_ADDRESS=0.0.0.0"
      [[ $require_https == false ]] ||
        die "direct-http requires FRUX_S3_REQUIRE_PUBLIC_HTTPS=false"
      web_port=$(prod_env_value_or FRUX_WEB_PORT 18080)
      minio_port=$(prod_env_value_or FRUX_MINIO_API_PORT 19000)
      [[ $app_port == "$web_port" ]] ||
        die "direct-http requires FRUX_PUBLIC_APP_PORT to match FRUX_WEB_PORT"
      [[ $s3_port == "$minio_port" ]] ||
        die "direct-http requires FRUX_PUBLIC_S3_PORT to match FRUX_MINIO_API_PORT"
      ;;
    *)
      die "FRUX_PUBLIC_ACCESS_MODE must be caddy-https or direct-http"
      ;;
  esac
}

wait_caddy_routes() {
  local domain
  local health
  local media_probe media_status
  local attempt

  domain=$(prod_hostname FRUX_DOMAIN)
  for ((attempt = 1; attempt <= FRUX_HEALTH_ATTEMPTS; attempt++)); do
    health=$(
      "$CURL_BIN" \
        --fail \
        --silent \
        --show-error \
        --max-time 10 \
        --resolve "$domain:443:127.0.0.1" \
        "https://$domain/health" 2>/dev/null || true
    )
    media_probe=$(
      "$CURL_BIN" \
        --silent \
        --show-error \
        --max-time 10 \
        --include \
        --write-out $'\n%{http_code}' \
        --resolve "$domain:443:127.0.0.1" \
        "https://$domain/media/processed/not-public.mp4" 2>/dev/null || true
    )
    media_status=${media_probe##*$'\n'}
    if grep -q '"ready":true' <<<"$health" &&
      [[ $media_status == 404 ]] &&
      grep -Eiq '^cache-control:[[:space:]]*private,[[:space:]]*no-store[[:space:]]*$' <<<"$media_probe" &&
      "$CURL_BIN" \
        --fail \
        --silent \
        --show-error \
        --max-time 10 \
        --resolve "$domain:443:127.0.0.1" \
        "https://$domain/" >/dev/null 2>&1; then
      return 0
    fi
    "$SLEEP_BIN" "$FRUX_HEALTH_SLEEP"
  done
  return 1
}

wait_direct_routes() {
  local host app_port web_port
  local health
  local media_probe media_status
  local attempt

  host=$(prod_env_value FRUX_DOMAIN)
  app_port=$(public_app_port)
  web_port=$(prod_env_value_or FRUX_WEB_PORT 18080)
  for ((attempt = 1; attempt <= FRUX_HEALTH_ATTEMPTS; attempt++)); do
    health=$(
      "$CURL_BIN" \
        --fail \
        --silent \
        --show-error \
        --max-time 10 \
        --header "Host: $host:$app_port" \
        "http://127.0.0.1:$web_port/health" 2>/dev/null || true
    )
    media_probe=$(
      "$CURL_BIN" \
        --silent \
        --show-error \
        --max-time 10 \
        --include \
        --write-out $'\n%{http_code}' \
        --header "Host: $host:$app_port" \
        "http://127.0.0.1:$web_port/media/processed/not-public.mp4" 2>/dev/null || true
    )
    media_status=${media_probe##*$'\n'}
    if grep -q '"ready":true' <<<"$health" &&
      [[ $media_status == 404 ]] &&
      grep -Eiq '^cache-control:[[:space:]]*private,[[:space:]]*no-store[[:space:]]*$' <<<"$media_probe" &&
      "$CURL_BIN" \
        --fail \
        --silent \
        --show-error \
        --max-time 10 \
        --header "Host: $host:$app_port" \
        "http://127.0.0.1:$web_port/" >/dev/null 2>&1; then
      return 0
    fi
    "$SLEEP_BIN" "$FRUX_HEALTH_SLEEP"
  done
  return 1
}

wait_public_routes() {
  case "$(prod_env_value_or FRUX_PUBLIC_ACCESS_MODE caddy-https)" in
    caddy-https) wait_caddy_routes ;;
    direct-http) wait_direct_routes ;;
    *) return 1 ;;
  esac
}

wait_worker_ready() {
  local release=$1
  local container metrics workflow_count
  local attempt

  wait_healthy "$release" worker || return 1
  for ((attempt = 1; attempt <= FRUX_HEALTH_ATTEMPTS; attempt++)); do
    container=$(compose_release "$release" --profile worker ps -q worker 2>/dev/null || true)
    if [[ -n $container ]]; then
      metrics=$(
        "$DOCKER_BIN" exec "$container" \
          wget -qO- http://127.0.0.1:9091/metrics 2>/dev/null || true
      )
      workflow_count=$(
        grep -Ec '^frux_kafka_consumer_workflow_healthy\{.*\} 1$' <<<"$metrics" || true
      )
      if grep -q '^frux_kafka_broker_healthy 1$' <<<"$metrics" &&
        [[ $workflow_count -ge 5 ]] &&
        ! grep -Eq '^frux_kafka_consumer_workflow_healthy\{.*\} 0$' <<<"$metrics"; then
        return 0
      fi
    fi
    "$SLEEP_BIN" "$FRUX_HEALTH_SLEEP"
  done
  return 1
}

wait_compose_ready() {
  local release=$1

  if multimodal_deployment_enabled; then
    wait_healthy "$release" multimodal-provider || return 1
  fi
  wait_healthy "$release" minio &&
    wait_healthy "$release" api &&
    wait_healthy "$release" web &&
    wait_healthy "$release" postgres-backup &&
    wait_worker_ready "$release"
}

validate_bundle() {
  local release=$1
  local actual expected manifest_actual manifest_expected

  [[ -z $(find "$release" -type l -print -quit) ]] ||
    die "deployment bundle contains a symbolic link"
  [[ -z $(find "$release" ! -type f ! -type d -print -quit) ]] ||
    die "deployment bundle contains an unsupported file type"

  actual=$(cd "$release" && find . -type f -printf '%P\n' | LC_ALL=C sort)
  expected=$(
    printf '%s\n' \
      apps/.env.prod.example \
      apps/.env.release \
      apps/api/configs/config.prod.yaml \
      apps/docker-compose.prod.yml \
      manifest.sha256 \
      scripts/postgres-backup.sh |
      LC_ALL=C sort
  )
  [[ $actual == "$expected" ]] || die "deployment bundle file list is invalid"

  manifest_actual=$(
    awk 'NF == 2 { print $2 }' "$release/manifest.sha256" |
      LC_ALL=C sort
  )
  manifest_expected=$(
    printf '%s\n' \
      apps/.env.prod.example \
      apps/.env.release \
      apps/api/configs/config.prod.yaml \
      apps/docker-compose.prod.yml \
      scripts/postgres-backup.sh |
      LC_ALL=C sort
  )
  [[ $manifest_actual == "$manifest_expected" ]] ||
    die "deployment checksum manifest is invalid"
  (cd "$release" && sha256sum -c manifest.sha256 >/dev/null) ||
    die "deployment checksum verification failed"

  [[ $(wc -l <"$release/apps/.env.release") -eq 3 ]] ||
    die "release environment has an unexpected shape"
  grep -Eq '^FRUX_API_IMAGE=ghcr\.io/shiyudesu/frux-api@sha256:[0-9a-f]{64}$' \
    "$release/apps/.env.release" || die "API image digest is invalid"
  grep -Eq '^FRUX_WEB_IMAGE=ghcr\.io/shiyudesu/frux-web@sha256:[0-9a-f]{64}$' \
    "$release/apps/.env.release" || die "Web image digest is invalid"
  grep -Eq '^FRUX_RELEASE_SHA=[0-9a-f]{40}$' \
    "$release/apps/.env.release" || die "release SHA is invalid"
}

restore_release_without_multimodal() {
  local previous=$1

  (
    export FRUX_DEPLOY_MULTIMODAL_OVERRIDE=false
    export FRUX_MULTIMODAL_PROFILE=
    export FRUX_MULTIMODAL_ENDPOINT=
    export FRUX_MULTIMODAL_HMAC_SECRET=
    export FRUX_MULTIMODAL_ENABLED=false
    export FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED=false
    export FRUX_MULTIMODAL_QUERY_EMBEDDING_ENABLED=false
    export FRUX_MULTIMODAL_HYBRID_SEARCH_ENABLED=false
    export FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED=false
    export FRUX_MULTIMODAL_SESSION_DEVELOPMENT_FULL_ROLLOUT_ENABLED=false
    export FRUX_MULTIMODAL_SESSION_PRODUCTION_FULL_ROLLOUT_ENABLED=false
    export FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK=false
    if release_supports_multimodal "$previous"; then
      compose_release "$previous" --profile multimodal rm -sf multimodal-provider >/dev/null 2>&1 || true
    fi
    compose_release "$previous" --profile worker pull api web worker || true
    compose_release "$previous" --profile worker up -d || return 1
    wait_compose_ready "$previous" && wait_public_routes
  )
}

restore_release() {
  local previous=$1
  local previous_multimodal_enabled=${2:-false}

  if [[ $previous_multimodal_enabled != true ]] ||
    ! multimodal_deployment_enabled ||
    ! release_supports_multimodal "$previous"; then
    restore_release_without_multimodal "$previous"
    return
  fi
  compose_release "$previous" --profile worker pull api web worker multimodal-provider || true
  if compose_release "$previous" --profile worker up -d &&
    wait_compose_ready "$previous" && wait_public_routes; then
    return 0
  fi
  echo "Previous multimodal profile was unhealthy; retrying rollback with multimodal disabled." >&2
  restore_release_without_multimodal "$previous"
}

prune_releases() {
  local current=$1
  local previous=${2:-}
  local candidate

  while IFS= read -r -d '' candidate; do
    [[ $candidate == "$current" || $candidate == "$previous" ]] && continue
    safe_release_path "$candidate" || die "refusing to prune an unsafe release path"
    rm -rf -- "$candidate"
  done < <(find "$FRUX_ROOT/releases" -mindepth 1 -maxdepth 1 -type d -print0)
}

prune_images() {
  local current=$1
  local previous=${2:-}
  local keep_file release repository reference digest_id

  keep_file=$(mktemp "$FRUX_ROOT/.keep-images.XXXXXX")
  for release in "$current" "$previous"; do
    [[ -n $release && -f "$release/apps/.env.release" ]] || continue
    awk -F= '/^FRUX_(API|WEB)_IMAGE=/{ print $2 }' "$release/apps/.env.release" >>"$keep_file"
    digest_id=$(basename "$release")
    if [[ $digest_id =~ ^sha256-([0-9a-f]{64})$ ]]; then
      printf 'ghcr.io/shiyudesu/frux-deploy@sha256:%s\n' "${BASH_REMATCH[1]}" >>"$keep_file"
    fi
  done
  LC_ALL=C sort -u -o "$keep_file" "$keep_file"

  for repository in \
    ghcr.io/shiyudesu/frux-api \
    ghcr.io/shiyudesu/frux-web \
    ghcr.io/shiyudesu/frux-deploy; do
    while IFS= read -r reference; do
      [[ $reference == "$repository"@sha256:* ]] || continue
      [[ ${reference##*@sha256:} =~ ^[0-9a-f]{64}$ ]] || continue
      grep -Fxq "$reference" "$keep_file" && continue
      "$DOCKER_BIN" image rm "$reference" >/dev/null 2>&1 || true
    done < <(
      "$DOCKER_BIN" image ls \
        --digests \
        --format '{{.Repository}}@{{.Digest}}' \
        "$repository" 2>/dev/null || true
    )
  done
  rm -f "$keep_file"
}

main() {
  local lock_file releases_dir prod_env current_link digest_file config_digest_file multimodal_state_file
  local digest_ref digest_id release_dir incoming container config_digest
  local previous_release previous_multimodal_enabled deploy_ok desired_multimodal_enabled
  local -a pull_services
  local link_temp digest_temp config_digest_temp multimodal_state_temp

  require_command "$DOCKER_BIN"
  require_command "$FLOCK_BIN"
  require_command "$CURL_BIN"
  require_command find
  require_command sha256sum

  [[ $FRUX_ROOT == /* && $FRUX_ROOT != / ]] || die "FRUX_ROOT must be an absolute non-root path"
  [[ $FRUX_DEPLOY_IMAGE =~ ^ghcr\.io/shiyudesu/frux-deploy:[A-Za-z0-9._-]+$ ]] ||
    die "FRUX_DEPLOY_IMAGE is invalid"
  [[ $FRUX_HEALTH_ATTEMPTS =~ ^[1-9][0-9]*$ ]] || die "FRUX_HEALTH_ATTEMPTS is invalid"
  [[ $FRUX_HEALTH_SLEEP =~ ^[0-9]+$ ]] || die "FRUX_HEALTH_SLEEP is invalid"

  releases_dir="$FRUX_ROOT/releases"
  prod_env="$FRUX_ROOT/.env.prod"
  current_link="$FRUX_ROOT/current"
  digest_file="$FRUX_ROOT/.deployed-digest"
  config_digest_file="$FRUX_ROOT/.deployed-config-digest"
  multimodal_state_file="$FRUX_ROOT/.deployed-multimodal"
  lock_file="$FRUX_ROOT/.deploy.lock"

  mkdir -p "$releases_dir"
  [[ -f $prod_env ]] || die "missing $prod_env"
  exec 9>"$lock_file"
  "$FLOCK_BIN" -n 9 || {
    echo "Another Frux deployment is running; skipping this check."
    exit 0
  }

  validate_public_access_config
  validate_multimodal_deployment_config
  config_digest=$(sha256sum "$prod_env" | awk '{print $1}')
  desired_multimodal_enabled=false
  if multimodal_deployment_enabled; then
    desired_multimodal_enabled=true
  fi
  previous_multimodal_enabled=false
  if [[ -f $multimodal_state_file ]] && [[ $(<"$multimodal_state_file") == true ]]; then
    previous_multimodal_enabled=true
  fi

  if [[ ! -L $current_link ]]; then
    if [[ -n $(
      "$DOCKER_BIN" ps -aq \
        --filter 'label=com.docker.compose.project=frux-prod' 2>/dev/null || true
    ) ]]; then
      die "existing unmanaged frux-prod containers found; stop them without deleting volumes before enabling the pull agent"
    fi
  fi

  "$DOCKER_BIN" pull "$FRUX_DEPLOY_IMAGE"
  digest_ref=$(
    "$DOCKER_BIN" image inspect \
      -f '{{range .RepoDigests}}{{println .}}{{end}}' \
      "$FRUX_DEPLOY_IMAGE" |
      while IFS= read -r reference; do
        digest=${reference##*@sha256:}
        if [[ $reference == *@sha256:* &&
          ${#digest} -eq 64 &&
          $digest =~ ^[0-9a-f]+$ ]]; then
          printf '%s\n' "$reference"
          break
        fi
      done
  )
  [[ $digest_ref =~ @sha256:([0-9a-f]{64})$ ]] ||
    die "could not resolve deployment image digest"
  digest_id="sha256-${BASH_REMATCH[1]}"

  if [[ -f $digest_file && -L $current_link ]] &&
    [[ $(<"$digest_file") == "$digest_ref" ]] &&
    [[ -f $config_digest_file && $(<"$config_digest_file") == "$config_digest" ]] &&
    [[ -f "$(readlink -f "$current_link")/apps/docker-compose.prod.yml" ]]; then
    echo "Prod deployment is already current."
    exit 0
  fi

  release_dir="$releases_dir/$digest_id"
  if [[ ! -d $release_dir ]]; then
    incoming=$(mktemp -d "$releases_dir/.incoming.XXXXXX")
    container=
    trap '
      if [[ -n ${container:-} ]]; then "$DOCKER_BIN" rm -f "$container" >/dev/null 2>&1 || true; fi
      if [[ -n ${incoming:-} && -d $incoming ]]; then rm -rf -- "$incoming"; fi
    ' EXIT
    container=$("$DOCKER_BIN" create "$FRUX_DEPLOY_IMAGE")
    "$DOCKER_BIN" cp "$container:/bundle/." "$incoming"
    "$DOCKER_BIN" rm "$container" >/dev/null
    container=
    validate_bundle "$incoming"
    mv "$incoming" "$release_dir"
    incoming=
    trap - EXIT
  else
    validate_bundle "$release_dir"
  fi
  validate_release_multimodal_support "$release_dir"

  previous_release=
  if [[ -L $current_link ]]; then
    previous_release=$(readlink -f "$current_link")
    safe_release_path "$previous_release" ||
      die "current release points outside the release directory"
  fi

  deploy_ok=true
  pull_services=(api web worker)
  if multimodal_deployment_enabled; then
    pull_services+=(multimodal-provider)
  fi
  compose_release "$release_dir" --profile worker pull "${pull_services[@]}" || deploy_ok=false
  if [[ $deploy_ok == true ]]; then
    compose_release "$release_dir" --profile worker up -d || deploy_ok=false
  fi
  if [[ $deploy_ok == true ]]; then
    wait_compose_ready "$release_dir" || deploy_ok=false
  fi
  if [[ $deploy_ok == true ]]; then
    wait_public_routes || deploy_ok=false
  fi

  if [[ $deploy_ok != true ]]; then
    echo "New Prod release is unhealthy; restoring the previous release." >&2
    compose_release "$release_dir" --profile multimodal rm -sf multimodal-provider >/dev/null 2>&1 || true
    if [[ -n $previous_release ]] && restore_release "$previous_release" "$previous_multimodal_enabled"; then
      echo "Previous Prod release restored." >&2
      if [[ $release_dir != "$previous_release" ]]; then
        safe_release_path "$release_dir" ||
          die "refusing to remove an unsafe failed release path"
        rm -rf -- "$release_dir"
      else
        echo "Current release bundle retained after failed configuration re-apply." >&2
      fi
    else
      compose_release "$release_dir" --profile worker down >/dev/null 2>&1 || true
      echo "No healthy previous release was restored; failed bundle retained at $release_dir." >&2
    fi
    exit 1
  fi

  link_temp="$FRUX_ROOT/.current.$$"
  digest_temp="$FRUX_ROOT/.deployed-digest.$$"
  config_digest_temp="$FRUX_ROOT/.deployed-config-digest.$$"
  multimodal_state_temp="$FRUX_ROOT/.deployed-multimodal.$$"
  ln -s "$release_dir" "$link_temp"
  mv -Tf "$link_temp" "$current_link"
  printf '%s\n' "$digest_ref" >"$digest_temp"
  printf '%s\n' "$config_digest" >"$config_digest_temp"
  printf '%s\n' "$desired_multimodal_enabled" >"$multimodal_state_temp"
  mv "$digest_temp" "$digest_file"
  mv "$config_digest_temp" "$config_digest_file"
  mv "$multimodal_state_temp" "$multimodal_state_file"
  prune_releases "$release_dir" "$previous_release"
  prune_images "$release_dir" "$previous_release"
  echo "Prod deployment updated to $digest_ref."
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main "$@"
fi
