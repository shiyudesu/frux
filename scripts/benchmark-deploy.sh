#!/usr/bin/env bash

set -Eeuo pipefail
set +x
umask 077

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [[ -n ${FRUX_BENCH_SOURCE_ROOT:-} ]]; then
  SOURCE_ROOT=$FRUX_BENCH_SOURCE_ROOT
elif [[ -f $SCRIPT_DIR/../apps/docker-compose.benchmark.yml ]]; then
  SOURCE_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
elif [[ -f $SCRIPT_DIR/apps/docker-compose.benchmark.yml ]]; then
  SOURCE_ROOT=$SCRIPT_DIR
else
  SOURCE_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
fi
BENCH_ROOT=${FRUX_BENCH_ROOT:-/data/data1/frux-benchmark}
PROD_RELEASE_ENV=${FRUX_PROD_RELEASE_ENV:-/opt/frux/current/apps/.env.release}
DOCKER_BIN=${DOCKER_BIN:-docker}
PROJECT_NAME=frux-benchmark

die() {
  printf 'Frux benchmark deployment failed: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command is unavailable: $1"
}

validate_root() {
  [[ $BENCH_ROOT == /* && $BENCH_ROOT != / && $BENCH_ROOT != /opt ]] ||
    die "FRUX_BENCH_ROOT must be a dedicated absolute directory"
}

source_file() {
  local relative=$1
  local path="$SOURCE_ROOT/$relative"
  [[ -f $path ]] || die "missing source file: $path"
  printf '%s' "$path"
}

random_secret() {
  openssl rand -base64 48 | tr -d '\n'
}

write_initial_env() {
  local target=$1
  local postgres_password redis_password consumer_secret admin_secret
  local hmac_secret internal_token viewer_password

  postgres_password=$(random_secret)
  redis_password=$(random_secret)
  consumer_secret=$(random_secret)
  admin_secret=$(random_secret)
  hmac_secret=$(random_secret)
  internal_token=$(random_secret)
  viewer_password=$(openssl rand -hex 16)

  {
    printf 'FRUX_BENCH_POSTGRES_USER=frux_bench\n'
    printf 'FRUX_BENCH_POSTGRES_PASSWORD=%s\n' "$postgres_password"
    printf 'FRUX_BENCH_POSTGRES_DATABASE=frux_benchmark\n'
    printf 'FRUX_BENCH_REDIS_PASSWORD=%s\n' "$redis_password"
    printf 'FRUX_BENCH_FEED_CACHE_MODE=batch\n'
    printf 'FRUX_BENCH_JWT_CONSUMER_SECRET=%s\n' "$consumer_secret"
    printf 'FRUX_BENCH_JWT_ADMIN_SECRET=%s\n' "$admin_secret"
    printf 'FRUX_BENCH_HMAC_SECRET=%s\n' "$hmac_secret"
    printf 'FRUX_BENCH_INTERNAL_TOKEN=%s\n' "$internal_token"
    printf 'FRUX_BENCH_VIEWER_PASSWORD=%s\n' "$viewer_password"
    printf 'FRUX_BENCH_API_PORT=28081\n'
    printf 'FRUX_BENCH_PROMETHEUS_PORT=29090\n'
    printf 'FRUX_BENCH_POSTGRES_CPUS=0.75\n'
    printf 'FRUX_BENCH_POSTGRES_MEMORY=2g\n'
    printf 'FRUX_BENCH_REDIS_CPUS=0.25\n'
    printf 'FRUX_BENCH_REDIS_MEMORY=1g\n'
    printf 'FRUX_BENCH_REDIS_MAXMEMORY=768mb\n'
    printf 'FRUX_BENCH_KAFKA_CPUS=0.75\n'
    printf 'FRUX_BENCH_KAFKA_MEMORY=2g\n'
    printf 'FRUX_BENCH_KAFKA_HEAP_OPTS=-Xms512m -Xmx1024m\n'
    printf 'FRUX_BENCH_API_CPUS=1.5\n'
    printf 'FRUX_BENCH_API_MEMORY=3g\n'
    printf 'FRUX_BENCH_WORKER_CPUS=1\n'
    printf 'FRUX_BENCH_WORKER_MEMORY=2g\n'
    printf 'FRUX_BENCH_PROMETHEUS_CPUS=0.25\n'
    printf 'FRUX_BENCH_PROMETHEUS_MEMORY=768m\n'
    printf 'FRUX_BENCH_K6_CPUS=1\n'
    printf 'FRUX_BENCH_K6_MEMORY=1g\n'
    printf 'FRUX_BENCH_SEED_CPUS=1\n'
    printf 'FRUX_BENCH_SEED_MEMORY=1g\n'
    printf 'FRUX_BENCH_KAFKA_RETENTION_HOURS=2\n'
    printf 'FRUX_BENCH_KAFKA_RETENTION_BYTES=134217728\n'
    printf 'FRUX_BENCH_KAFKA_SEGMENT_BYTES=67108864\n'
    printf 'FRUX_BENCH_PROMETHEUS_RETENTION=48h\n'
    printf 'FRUX_BENCH_PROMETHEUS_SIZE=512MB\n'
    printf 'FRUX_BENCH_SEED_USERS=12000\n'
    printf 'FRUX_BENCH_SEED_AUTHORS=500\n'
    printf 'FRUX_BENCH_SEED_VIDEOS=50000\n'
    printf 'FRUX_BENCH_SEED_FOLLOWS_PER_USER=30\n'
    printf 'FRUX_BENCH_K6_IMAGE=grafana/k6:2.2.0\n'
    printf 'FRUX_BENCH_RESULTS_DIR=%s/results\n' "$BENCH_ROOT"
  } >"$target"
  chmod 600 "$target"
}

install_bundle() {
  local release_env=$PROD_RELEASE_ENV
  local script_source

  validate_root
  [[ -f $release_env ]] || die "missing immutable Prod release environment: $release_env"
  grep -Eq '^FRUX_API_IMAGE=ghcr\.io/shiyudesu/frux-api@sha256:[0-9a-f]{64}$' "$release_env" ||
    die "Prod release environment does not contain an immutable API image"

  install -d -m 700 \
    "$BENCH_ROOT" \
    "$BENCH_ROOT/bin" \
    "$BENCH_ROOT/apps/api/configs" \
    "$BENCH_ROOT/apps/benchmark/k6" \
    "$BENCH_ROOT/results"
  install -m 600 "$release_env" "$BENCH_ROOT/.env.release"
  install -m 600 "$(source_file apps/docker-compose.benchmark.yml)" \
    "$BENCH_ROOT/apps/docker-compose.benchmark.yml"
  install -m 600 "$(source_file apps/api/configs/config.benchmark.yaml)" \
    "$BENCH_ROOT/apps/api/configs/config.benchmark.yaml"
  install -m 600 "$(source_file apps/benchmark/prometheus.yml)" \
    "$BENCH_ROOT/apps/benchmark/prometheus.yml"
  install -m 600 "$(source_file apps/benchmark/Dockerfile.runtime)" \
    "$BENCH_ROOT/apps/benchmark/Dockerfile.runtime"
  install -m 600 "$(source_file apps/benchmark/seed.sql)" \
    "$BENCH_ROOT/apps/benchmark/seed.sql"
  install -m 600 "$(source_file apps/benchmark/following_index.sql)" \
    "$BENCH_ROOT/apps/benchmark/following_index.sql"
  install -m 600 "$(source_file apps/benchmark/summarize-fanout-ab.mjs)" \
    "$BENCH_ROOT/apps/benchmark/summarize-fanout-ab.mjs"
  install -m 600 "$(source_file apps/benchmark/k6/feed.js)" \
    "$BENCH_ROOT/apps/benchmark/k6/feed.js"
  script_source=$(source_file scripts/benchmark-deploy.sh)
  if [[ $script_source != "$BENCH_ROOT/benchmark-deploy.sh" ]]; then
    install -m 755 "$script_source" "$BENCH_ROOT/benchmark-deploy.sh"
  fi

  if [[ ! -f $BENCH_ROOT/.env.benchmark ]]; then
    write_initial_env "$BENCH_ROOT/.env.benchmark"
    printf 'Created %s with generated isolated credentials.\n' "$BENCH_ROOT/.env.benchmark"
  fi
}

compose() {
  local -a env_files
  env_files=(
    --env-file "$BENCH_ROOT/.env.benchmark"
    --env-file "$BENCH_ROOT/.env.release"
  )
  if [[ -f $BENCH_ROOT/.env.runtime ]]; then
    env_files+=(--env-file "$BENCH_ROOT/.env.runtime")
  fi
  if [[ -f $BENCH_ROOT/.env.feed-mode ]]; then
    env_files+=(--env-file "$BENCH_ROOT/.env.feed-mode")
  fi
  "$DOCKER_BIN" compose \
    "${env_files[@]}" \
    -p "$PROJECT_NAME" \
    -f "$BENCH_ROOT/apps/docker-compose.benchmark.yml" \
    "$@"
}

build_runtime() {
  local api_bin="$BENCH_ROOT/bin/frux-api"
  local worker_bin="$BENCH_ROOT/bin/frux-worker"
  local base_image runtime_hash runtime_tag
  [[ -x $api_bin && -x $worker_bin ]] || die "copy executable frux-api and frux-worker into $BENCH_ROOT/bin first"
  base_image=$(sed -n 's/^FRUX_API_IMAGE=//p' "$BENCH_ROOT/.env.release" | tail -n 1)
  [[ $base_image == ghcr.io/shiyudesu/frux-api@sha256:* ]] || die "invalid immutable base image"
  runtime_hash=$(
    sha256sum "$api_bin" "$worker_bin" |
      sha256sum |
      cut -c1-16
  )
  runtime_tag="frux-benchmark-runtime:following-index-v2-$runtime_hash"
  "$DOCKER_BIN" build \
    --build-arg "BASE_IMAGE=$base_image" \
    -f "$BENCH_ROOT/apps/benchmark/Dockerfile.runtime" \
    -t "$runtime_tag" \
    "$BENCH_ROOT"
  printf 'FRUX_API_IMAGE=%s\n' "$runtime_tag" >"$BENCH_ROOT/.env.runtime"
  chmod 600 "$BENCH_ROOT/.env.runtime"
  compose up -d --force-recreate --wait --wait-timeout 240 api worker
  verify_model_isolation
  "$DOCKER_BIN" image inspect --format '{{.Id}}' "$runtime_tag"
}

verify_model_isolation() {
  local service
  for service in api worker; do
    compose exec -T "$service" sh -ec '
      test -z "${DASHSCOPE_API_KEY+x}"
      test -z "${FRUX_MULTIMODAL_ENDPOINT+x}"
      test "$FRUX_MULTIMODAL_ENABLED" = false
      test "$FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED" = false
      test "$FRUX_MULTIMODAL_QUERY_EMBEDDING_ENABLED" = false
      test "$FRUX_MULTIMODAL_HYBRID_SEARCH_ENABLED" = false
      test "$FRUX_MULTIMODAL_SESSION_RECOMMENDATION_ENABLED" = false
    '
  done
}

start_stack() {
  if [[ ! -f $BENCH_ROOT/.env.benchmark || ! -f $BENCH_ROOT/.env.release ]]; then
    install_bundle
  fi
  compose up -d --wait --wait-timeout 240 postgres redis kafka api worker
  verify_model_isolation
  printf 'Benchmark API: http://127.0.0.1:%s\n' "$(sed -n 's/^FRUX_BENCH_API_PORT=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)"
  printf 'Start optional Prometheus with: %s start-monitoring\n' "$0"
}

start_monitoring() {
  compose --profile monitoring up -d --wait --wait-timeout 180 prometheus
  printf 'Benchmark Prometheus: http://127.0.0.1:%s\n' "$(sed -n 's/^FRUX_BENCH_PROMETHEUS_PORT=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)"
}

seed_stack() {
  local postgres_user postgres_database export_file

  compose --profile seed run --rm seed
  postgres_user=$(sed -n 's/^FRUX_BENCH_POSTGRES_USER=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  postgres_database=$(sed -n 's/^FRUX_BENCH_POSTGRES_DATABASE=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  export_file="$BENCH_ROOT/results/following-index.tsv"
  compose exec -T postgres psql \
    -qAt \
    -F '|' \
    -U "$postgres_user" \
    -d "$postgres_database" \
    -f - \
    <"$BENCH_ROOT/apps/benchmark/following_index.sql" \
    >"$export_file"
  chmod 600 "$export_file"
  compose exec -T redis sh -ec '
    redis_cli() {
      redis-cli -a "$REDIS_PASSWORD" --no-auth-warning "$@"
    }
    redis_cli DEL feed:following:inbox:v2:1 >/dev/null
    redis_cli DEL feed:following:author:v2:2 >/dev/null
    while IFS="|" read -r key score member; do
      test -n "$key" || continue
      redis_cli ZADD "$key" "$score" "$member" >/dev/null
    done </benchmark-results/following-index.tsv
    redis_cli EXPIRE feed:following:inbox:v2:1 2592000 >/dev/null
    redis_cli EXPIRE feed:following:author:v2:2 2592000 >/dev/null
  '
  printf 'Seeded the isolated PostgreSQL fixtures and Redis push/pull indexes.\n'
}

run_feed() {
  local scene=${1:-timeline}
  local vus=${2:-20}
  local duration=${3:-60s}
  local limit=${4:-20}
  local label=${5:-baseline}
  local first_page_percent=${6:-80}
  local stamp result_file viewer_password k6_bin api_port
  local redis_before redis_after postgres_before postgres_after api_before api_after

  [[ $scene == timeline || $scene == hot || $scene == following || $scene == recommend ]] ||
    die "scene must be timeline, hot, following, or recommend"
  [[ $vus =~ ^[1-9][0-9]*$ ]] || die "VUS must be a positive integer"
  [[ $limit =~ ^[1-9][0-9]*$ && $limit -le 100 ]] || die "limit must be between 1 and 100"
  [[ $duration =~ ^[1-9][0-9]*(s|m|h)$ ]] || die "duration must be a simple positive duration"
  [[ $label =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "label must contain only lowercase letters, digits, underscore, and hyphen"
  [[ $first_page_percent == 20 || $first_page_percent == 80 || $first_page_percent == 100 ]] ||
    die "first-page-percent must be 20, 80, or 100"

  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  result_file="feed-${scene}-${label}-first${first_page_percent}-vu${vus}-${duration}-${stamp}.json"
  redis_before="$BENCH_ROOT/results/${result_file%.json}-redis-before.txt"
  redis_after="$BENCH_ROOT/results/${result_file%.json}-redis-after.txt"
  postgres_before="$BENCH_ROOT/results/${result_file%.json}-postgres-before.txt"
  postgres_after="$BENCH_ROOT/results/${result_file%.json}-postgres-after.txt"
  api_before="$BENCH_ROOT/results/${result_file%.json}-api-before.prom"
  api_after="$BENCH_ROOT/results/${result_file%.json}-api-after.prom"
  capture_redis_info "$redis_before"
  capture_postgres_stats "$postgres_before"
  capture_api_metrics "$api_before"
  k6_bin="$BENCH_ROOT/bin/k6"
  if [[ -x $k6_bin ]]; then
    viewer_password=$(sed -n 's/^FRUX_BENCH_VIEWER_PASSWORD=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
    api_port=$(sed -n 's/^FRUX_BENCH_API_PORT=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
    env \
      BASE_URL="http://127.0.0.1:$api_port" \
      SCENE="$scene" \
      VUS="$vus" \
      DURATION="$duration" \
      LIMIT="$limit" \
      FIRST_PAGE_PERCENT="$first_page_percent" \
      THINK_TIME=0 \
      ACCOUNT=bench_viewer \
      PASSWORD="$viewer_password" \
      "$k6_bin" run --quiet --no-color \
      --summary-export="$BENCH_ROOT/results/$result_file" \
      "$BENCH_ROOT/apps/benchmark/k6/feed.js"
  else
    compose --profile load run --rm \
      -e SCENE="$scene" \
      -e VUS="$vus" \
      -e DURATION="$duration" \
      -e LIMIT="$limit" \
      -e FIRST_PAGE_PERCENT="$first_page_percent" \
      k6 run --summary-export="/results/$result_file" /scripts/feed.js
  fi
  capture_redis_info "$redis_after"
  capture_postgres_stats "$postgres_after"
  capture_api_metrics "$api_after"
  printf 'Saved k6 summary to %s/results/%s\n' "$BENCH_ROOT" "$result_file"
}

run_feed_burst() {
  local scene=${1:-timeline}
  local vus=${2:-100}
  local iterations=${3:-100}
  local limit=${4:-20}
  local label=${5:-burst}
  local stamp result_file k6_bin api_port
  local redis_before redis_after postgres_before postgres_after api_before api_after

  [[ $scene == timeline || $scene == hot ]] || die "burst scene must be timeline or hot"
  [[ $vus =~ ^[1-9][0-9]*$ && $iterations =~ ^[1-9][0-9]*$ ]] || die "burst VUs and iterations must be positive"
  [[ $limit =~ ^[1-9][0-9]*$ && $limit -le 100 ]] || die "limit must be between 1 and 100"
  [[ $label =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "invalid burst label"
  k6_bin="$BENCH_ROOT/bin/k6"
  [[ -x $k6_bin ]] || die "missing $k6_bin"
  api_port=$(sed -n 's/^FRUX_BENCH_API_PORT=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  result_file="feed-${scene}-${label}-first100-vu${vus}-iter${iterations}-$stamp.json"
  redis_before="$BENCH_ROOT/results/${result_file%.json}-redis-before.txt"
  redis_after="$BENCH_ROOT/results/${result_file%.json}-redis-after.txt"
  postgres_before="$BENCH_ROOT/results/${result_file%.json}-postgres-before.txt"
  postgres_after="$BENCH_ROOT/results/${result_file%.json}-postgres-after.txt"
  api_before="$BENCH_ROOT/results/${result_file%.json}-api-before.prom"
  api_after="$BENCH_ROOT/results/${result_file%.json}-api-after.prom"
  capture_redis_info "$redis_before"
  capture_postgres_stats "$postgres_before"
  capture_api_metrics "$api_before"
  env \
    BASE_URL="http://127.0.0.1:$api_port" \
    SCENE="$scene" \
    VUS="$vus" \
    ITERATIONS="$iterations" \
    LIMIT="$limit" \
    FIRST_PAGE_PERCENT=100 \
    THINK_TIME=0 \
    "$k6_bin" run --quiet --no-color \
    --summary-export="$BENCH_ROOT/results/$result_file" \
    "$BENCH_ROOT/apps/benchmark/k6/feed.js"
  capture_redis_info "$redis_after"
  capture_postgres_stats "$postgres_after"
  capture_api_metrics "$api_after"
  printf 'Saved burst summary to %s/results/%s\n' "$BENCH_ROOT" "$result_file"
}

run_fanout() {
  local events=${1:-100}
  local big_every=${2:-5}
  local label=${3:-baseline}
  local publisher_bin="$BENCH_ROOT/bin/benchmark-publish"
  local api_container stamp result_file author_count strategy

  [[ $events =~ ^[1-9][0-9]*$ ]] || die "events must be a positive integer"
  [[ $big_every =~ ^[1-9][0-9]*$ ]] || die "big-every must be a positive integer"
  [[ $label =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "label must contain only lowercase letters, digits, underscore, and hyphen"
  [[ -x $publisher_bin ]] || die "missing $publisher_bin"
  api_container=$(compose ps -q api)
  [[ -n $api_container ]] || die "benchmark API container is not running"
  author_count=$(sed -n 's/^FRUX_BENCH_SEED_AUTHORS=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  "$DOCKER_BIN" cp "$publisher_bin" "$api_container:/tmp/benchmark-publish" >/dev/null
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  strategy=$(current_fanout_strategy)
  result_file="$BENCH_ROOT/results/kafka-fanout-${strategy}-${label}-${events}-events-$stamp.json"
  "$DOCKER_BIN" exec "$api_container" /tmp/benchmark-publish \
    -events "$events" \
    -big-every "$big_every" \
    -author-count "$author_count" \
    2>&1 | tee "$result_file"
  chmod 600 "$result_file"
}

capture_redis_info() {
  local target=$1
  compose exec -T redis sh -ec '
    redis-cli -a "$REDIS_PASSWORD" --no-auth-warning INFO commandstats
    redis-cli -a "$REDIS_PASSWORD" --no-auth-warning INFO stats
    redis-cli -a "$REDIS_PASSWORD" --no-auth-warning INFO memory
  ' >"$target"
  chmod 600 "$target"
}

capture_postgres_stats() {
  local target=$1
  local postgres_user postgres_database
  postgres_user=$(sed -n 's/^FRUX_BENCH_POSTGRES_USER=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  postgres_database=$(sed -n 's/^FRUX_BENCH_POSTGRES_DATABASE=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  compose exec -T postgres psql -qAt -F '|' -U "$postgres_user" -d "$postgres_database" -c '
    SELECT xact_commit, xact_rollback, blks_read, blks_hit,
           tup_returned, tup_fetched, temp_files, temp_bytes, deadlocks
    FROM pg_stat_database
    WHERE datname = current_database()
  ' >"$target"
  chmod 600 "$target"
}

capture_api_metrics() {
  local target=$1
  compose exec -T api wget -qO- http://127.0.0.1:8080/metrics >"$target"
  chmod 600 "$target"
}

clear_timeline_cache() {
  compose exec -T redis sh -ec '
    redis_cli() {
      redis-cli -a "$REDIS_PASSWORD" --no-auth-warning "$@"
    }
    for pattern in "feed:page:*" "video:card:*" "video:stat:*"; do
      redis_cli --scan --pattern "$pattern" | while IFS= read -r key; do
        test -n "$key" && redis_cli UNLINK "$key" >/dev/null
      done
    done
  '
}

warm_timeline_cache() {
  local k6_bin="$BENCH_ROOT/bin/k6"
  local limit=${1:-20}
  local api_port
  [[ -x $k6_bin ]] || die "missing $k6_bin"
  api_port=$(sed -n 's/^FRUX_BENCH_API_PORT=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  env \
    BASE_URL="http://127.0.0.1:$api_port" \
    SCENE=timeline \
    VUS=1 \
    ITERATIONS=5 \
    LIMIT="$limit" \
    FIRST_PAGE_PERCENT=20 \
    THINK_TIME=0 \
    "$k6_bin" run --quiet --no-color "$BENCH_ROOT/apps/benchmark/k6/feed.js" >/dev/null
}

prepare_feed_cache_mode() {
  local mode=$1
  local warm=${2:-true}
  local limit=${3:-20}
  [[ $mode == disabled || $mode == sequential || $mode == batch ]] || die "feed cache mode must be disabled, sequential, or batch"
  printf 'FRUX_BENCH_FEED_CACHE_MODE=%s\n' "$mode" >"$BENCH_ROOT/.env.feed-mode"
  chmod 600 "$BENCH_ROOT/.env.feed-mode"
  compose up -d --force-recreate --no-deps --wait --wait-timeout 180 api
  verify_model_isolation
  clear_timeline_cache
  if [[ $warm == true ]]; then
    warm_timeline_cache "$limit"
  fi
  printf 'Prepared feed cache mode=%s warm=%s.\n' "$mode" "$warm"
}

run_feed_cache_ab() {
  local vus=${1:-20}
  local duration=${2:-60s}
  local limit=${3:-20}
  local rounds=${4:-3}
  local round mode
  local -a modes
  [[ $rounds =~ ^[1-9][0-9]*$ ]] || die "rounds must be a positive integer"
  trap 'prepare_feed_cache_mode batch true 20 >/dev/null 2>&1 || true' EXIT
  for ((round = 1; round <= rounds; round++)); do
    case $((round % 3)) in
      1) modes=(disabled sequential batch) ;;
      2) modes=(batch disabled sequential) ;;
      0) modes=(sequential batch disabled) ;;
    esac
    for mode in "${modes[@]}"; do
      prepare_feed_cache_mode "$mode" true "$limit"
      run_feed timeline "$vus" "$duration" "$limit" "cache-${mode}-round${round}" 80
    done
  done
  prepare_feed_cache_mode batch true "$limit"
  trap - EXIT
}

run_feed_page_size_matrix() {
  local vus=${1:-20}
  local duration=${2:-30s}
  local rounds=${3:-3}
  local round limit
  for ((round = 1; round <= rounds; round++)); do
    for limit in 10 50 100; do
      prepare_feed_cache_mode batch true "$limit"
      run_feed timeline "$vus" "$duration" "$limit" "page${limit}-round${round}" 80
    done
  done
}

run_feed_mget_short() {
  local rounds=${1:-3}
  local round mode
  [[ $rounds =~ ^[1-9][0-9]*$ ]] || die "rounds must be a positive integer"
  trap 'prepare_feed_cache_mode batch true 20 >/dev/null 2>&1 || true' EXIT
  for ((round = 1; round <= rounds; round++)); do
    for mode in sequential batch; do
      prepare_feed_cache_mode "$mode" true 20
      run_feed timeline 20 10s 20 "mget-${mode}-round${round}" 80
    done
  done
  prepare_feed_cache_mode batch true 20
  trap - EXIT
}

run_feed_cache_bursts() {
  local rounds=${1:-3}
  local round state
  for ((round = 1; round <= rounds; round++)); do
    for state in cold warm; do
      if [[ $state == cold ]]; then
        prepare_feed_cache_mode batch false 20
      else
        prepare_feed_cache_mode batch true 20
      fi
      run_feed_burst timeline 100 100 20 "${state}-round${round}"
    done
  done
  prepare_feed_cache_mode batch true 20
}

run_feed_cache_suite() {
  run_feed_cache_ab 20 60s 20 3
  run_feed_mget_short 3
  run_feed_page_size_matrix 20 30s 3
  run_feed_cache_bursts 3
}

current_fanout_strategy() {
  local postgres_user postgres_database count
  postgres_user=$(sed -n 's/^FRUX_BENCH_POSTGRES_USER=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  postgres_database=$(sed -n 's/^FRUX_BENCH_POSTGRES_DATABASE=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  count=$(compose exec -T postgres psql -qAt -U "$postgres_user" -d "$postgres_database" \
    -c 'SELECT follower_count FROM user_relation_stat WHERE user_id = 2')
  if (( count >= 10000 )); then
    printf 'hybrid'
  else
    printf 'all_push'
  fi
}

prepare_fanout_strategy() {
  local strategy=$1
  local postgres_user postgres_database statement actual_count routing_count
  [[ $strategy == all_push || $strategy == hybrid ]] || die "strategy must be all_push or hybrid"
  postgres_user=$(sed -n 's/^FRUX_BENCH_POSTGRES_USER=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  postgres_database=$(sed -n 's/^FRUX_BENCH_POSTGRES_DATABASE=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  if [[ $strategy == hybrid ]]; then
    statement='UPDATE user_relation_stat SET follower_count = (SELECT COUNT(*) FROM user_follow WHERE target_user_id = 2 AND status = 1), updated_at = NOW() WHERE user_id = 2'
  else
    statement='UPDATE user_relation_stat SET follower_count = 9999, updated_at = NOW() WHERE user_id = 2'
  fi
  compose exec -T postgres psql -qAt -U "$postgres_user" -d "$postgres_database" -c "$statement" >/dev/null
  actual_count=$(compose exec -T postgres psql -qAt -U "$postgres_user" -d "$postgres_database" \
    -c 'SELECT COUNT(*) FROM user_follow WHERE target_user_id = 2 AND status = 1')
  routing_count=$(compose exec -T postgres psql -qAt -U "$postgres_user" -d "$postgres_database" \
    -c 'SELECT follower_count FROM user_relation_stat WHERE user_id = 2')
  [[ $actual_count =~ ^[0-9]+$ && $routing_count =~ ^[0-9]+$ ]] || die "failed to verify fanout strategy"
  if [[ $strategy == hybrid ]]; then
    (( routing_count >= 10000 )) || die "hybrid strategy did not cross the big-creator threshold"
  else
    (( routing_count < 10000 && actual_count >= 10000 )) || die "all-push strategy fixture is invalid"
  fi
  compose exec -T redis sh -ec '
    redis-cli -a "$REDIS_PASSWORD" --no-auth-warning FLUSHDB >/dev/null
  '
  printf 'Prepared %s: actual_followers=%s routing_followers=%s and flushed only the isolated benchmark Redis DB.\n' \
    "$strategy" "$actual_count" "$routing_count"
}

restore_hybrid_routing() {
  local postgres_user postgres_database
  postgres_user=$(sed -n 's/^FRUX_BENCH_POSTGRES_USER=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  postgres_database=$(sed -n 's/^FRUX_BENCH_POSTGRES_DATABASE=//p' "$BENCH_ROOT/.env.benchmark" | tail -n 1)
  compose exec -T postgres psql -qAt -U "$postgres_user" -d "$postgres_database" -c \
    'UPDATE user_relation_stat SET follower_count = (SELECT COUNT(*) FROM user_follow WHERE target_user_id = 2 AND status = 1), updated_at = NOW() WHERE user_id = 2' \
    >/dev/null
}

run_fanout_ab() {
  local events=${1:-100}
  local big_every=${2:-5}
  local vus=${3:-20}
  local duration=${4:-60s}
  local limit=${5:-20}
  local rounds=${6:-3}
  local round strategy

  [[ $rounds =~ ^[1-9][0-9]*$ ]] || die "rounds must be a positive integer"
  trap 'restore_hybrid_routing >/dev/null 2>&1 || true' EXIT
  for ((round = 1; round <= rounds; round++)); do
    for strategy in all_push hybrid; do
      prepare_fanout_strategy "$strategy"
      run_fanout "$events" "$big_every" "round${round}"
      run_feed following "$vus" "$duration" "$limit" "${strategy}-round${round}" 80
    done
  done
  restore_hybrid_routing
  trap - EXIT
  printf 'Completed %s paired all-push/hybrid rounds and retained the final hybrid read fixture.\n' "$rounds"
}

destroy_stack() {
  [[ ${FRUX_BENCH_CONFIRM_DESTROY:-} == "$PROJECT_NAME" ]] ||
    die "set FRUX_BENCH_CONFIRM_DESTROY=$PROJECT_NAME to delete benchmark volumes"
  compose down -v --remove-orphans
}

main() {
  local action=${1:-status}

  require_command "$DOCKER_BIN"
  require_command openssl
  require_command install
  validate_root

  case "$action" in
    install) install_bundle ;;
    start) start_stack ;;
    build-runtime) build_runtime ;;
    start-monitoring) start_monitoring ;;
    seed) seed_stack ;;
    run-feed) shift; run_feed "$@" ;;
    run-feed-burst) shift; run_feed_burst "$@" ;;
    prepare-feed-cache) shift; prepare_feed_cache_mode "$@" ;;
    run-feed-mget-short) shift; run_feed_mget_short "$@" ;;
    run-feed-cache-suite) run_feed_cache_suite ;;
    run-fanout) shift; run_fanout "$@" ;;
    run-fanout-ab) shift; run_fanout_ab "$@" ;;
    verify) verify_model_isolation ;;
    status) compose ps ;;
    logs) compose logs --tail=200 api worker postgres redis kafka prometheus ;;
    stop) compose stop ;;
    down) compose down --remove-orphans ;;
    destroy) destroy_stack ;;
    *) die "usage: $0 {install|build-runtime|start|start-monitoring|seed|prepare-feed-cache [mode] [warm] [limit]|run-feed [scene] [vus] [duration] [limit] [label] [first-page-percent]|run-feed-burst [scene] [vus] [iterations] [limit] [label]|run-feed-mget-short [rounds]|run-feed-cache-suite|run-fanout [events] [big-every] [label]|run-fanout-ab [events] [big-every] [vus] [duration] [limit] [rounds]|verify|status|logs|stop|down|destroy}" ;;
  esac
}

main "$@"
