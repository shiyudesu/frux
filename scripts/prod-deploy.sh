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
  shift
  "$DOCKER_BIN" compose \
    --env-file "$FRUX_ROOT/.env.prod" \
    --env-file "$release/apps/.env.release" \
    -p frux-prod \
    -f "$release/apps/docker-compose.prod.yml" \
    "$@"
}

worker_is_running() {
  local release=$1
  local container
  container=$(compose_release "$release" --profile worker ps -q worker 2>/dev/null || true)
  [[ -n $container ]] &&
    [[ $("$DOCKER_BIN" inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true) == true ]]
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

prod_domain() {
  local value
  value=$(sed -n 's/^FRUX_DOMAIN=//p' "$FRUX_ROOT/.env.prod" | tail -n 1)
  [[ $value =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] &&
    [[ $value == *.* ]] &&
    [[ $value != *..* ]] ||
    die "FRUX_DOMAIN in .env.prod must be an unquoted hostname"
  printf '%s' "$value"
}

wait_caddy_routes() {
  local domain
  local attempt

  domain=$(prod_domain)
  for ((attempt = 1; attempt <= FRUX_HEALTH_ATTEMPTS; attempt++)); do
    if "$CURL_BIN" \
      --fail \
      --silent \
      --show-error \
      --max-time 10 \
      --resolve "$domain:443:127.0.0.1" \
      "https://$domain/health" >/dev/null 2>&1 &&
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

wait_release_ready() {
  local release=$1
  local worker_running=$2

  wait_healthy "$release" api &&
    wait_healthy "$release" web &&
    wait_healthy "$release" caddy &&
    wait_healthy "$release" postgres-backup &&
    wait_caddy_routes &&
    {
      [[ $worker_running != true ]] || wait_worker_ready "$release"
    }
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
      apps/Caddyfile.prod \
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
      apps/Caddyfile.prod \
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

restore_release() {
  local previous=$1
  local worker_running=$2

  compose_release "$previous" pull api web || true
  compose_release "$previous" up -d || return 1
  if [[ $worker_running == true ]]; then
    compose_release "$previous" --profile worker pull worker || true
    compose_release "$previous" --profile worker up -d worker || return 1
  fi
  wait_release_ready "$previous" "$worker_running"
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
  local lock_file releases_dir prod_env current_link digest_file
  local digest_ref digest_id release_dir incoming container
  local previous_release worker_running deploy_ok
  local link_temp digest_temp

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
  lock_file="$FRUX_ROOT/.deploy.lock"

  mkdir -p "$releases_dir"
  [[ -f $prod_env ]] || die "missing $prod_env"
  exec 9>"$lock_file"
  "$FLOCK_BIN" -n 9 || {
    echo "Another Frux deployment is running; skipping this check."
    exit 0
  }

  if [[ ! -L $current_link ]]; then
    if [[ -n $(
      "$DOCKER_BIN" ps -aq \
        --filter 'label=com.docker.compose.project=frux-prod' 2>/dev/null || true
    ) ]]; then
      die "existing unmanaged frux-prod containers found; stop them without deleting volumes before enabling the pull agent"
    fi
  fi

  "$DOCKER_BIN" pull "$FRUX_DEPLOY_IMAGE" >/dev/null
  digest_ref=$(
    "$DOCKER_BIN" image inspect \
      -f '{{range .RepoDigests}}{{println .}}{{end}}' \
      "$FRUX_DEPLOY_IMAGE" |
      awk '/@sha256:[0-9a-f]{64}$/ { print; exit }'
  )
  [[ $digest_ref =~ @sha256:([0-9a-f]{64})$ ]] ||
    die "could not resolve deployment image digest"
  digest_id="sha256-${BASH_REMATCH[1]}"

  if [[ -f $digest_file && -L $current_link ]] &&
    [[ $(<"$digest_file") == "$digest_ref" ]] &&
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

  previous_release=
  worker_running=false
  if [[ -L $current_link ]]; then
    previous_release=$(readlink -f "$current_link")
    safe_release_path "$previous_release" ||
      die "current release points outside the release directory"
    worker_is_running "$previous_release" && worker_running=true
  fi

  deploy_ok=true
  compose_release "$release_dir" pull api web || deploy_ok=false
  if [[ $deploy_ok == true ]]; then
    compose_release "$release_dir" up -d || deploy_ok=false
  fi
  if [[ $deploy_ok == true && $worker_running == true ]]; then
    compose_release "$release_dir" --profile worker pull worker || deploy_ok=false
    if [[ $deploy_ok == true ]]; then
      compose_release "$release_dir" --profile worker up -d worker || deploy_ok=false
    fi
  fi
  if [[ $deploy_ok == true ]] &&
    ! wait_release_ready "$release_dir" "$worker_running"; then
    deploy_ok=false
  fi

  if [[ $deploy_ok != true ]]; then
    echo "New Prod release is unhealthy; restoring the previous release." >&2
    if [[ -n $previous_release ]] && restore_release "$previous_release" "$worker_running"; then
      echo "Previous Prod release restored." >&2
      safe_release_path "$release_dir" ||
        die "refusing to remove an unsafe failed release path"
      rm -rf -- "$release_dir"
    else
      compose_release "$release_dir" --profile worker down >/dev/null 2>&1 || true
      echo "No healthy previous release was restored; failed bundle retained at $release_dir." >&2
    fi
    exit 1
  fi

  link_temp="$FRUX_ROOT/.current.$$"
  digest_temp="$FRUX_ROOT/.deployed-digest.$$"
  ln -s "$release_dir" "$link_temp"
  mv -Tf "$link_temp" "$current_link"
  printf '%s\n' "$digest_ref" >"$digest_temp"
  mv "$digest_temp" "$digest_file"
  prune_releases "$release_dir" "$previous_release"
  prune_images "$release_dir" "$previous_release"
  echo "Prod deployment updated to $digest_ref."
}

main "$@"
