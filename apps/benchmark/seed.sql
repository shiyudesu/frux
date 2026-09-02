\set ON_ERROR_STOP on
\timing on

CREATE EXTENSION IF NOT EXISTS pgcrypto;

SELECT
  :'users'::integer AS users,
  :'authors'::integer AS authors,
  :'videos'::integer AS videos,
  :'follows_per_user'::integer AS follows_per_user
\gset benchmark_

SELECT (
  :benchmark_users >= 10001
  AND :benchmark_authors >= 2
  AND :benchmark_authors <= :benchmark_users
  AND :benchmark_videos > 0
  AND :benchmark_follows_per_user > 0
  AND :benchmark_follows_per_user < :benchmark_authors
) AS valid_benchmark_shape
\gset

\if :valid_benchmark_shape
\else
  \echo 'invalid benchmark shape: require users >= 10001, 2 <= authors <= users, videos > 0, and 0 < follows_per_user < authors'
  \quit 3
\endif

WITH password_hash AS MATERIALIZED (
  SELECT crypt(:'benchmark_password', gen_salt('bf', 4)) AS value
)
INSERT INTO account (
  id, account, password, nickname, avatar_url, bio, gender, status, role,
  auth_version, created_at, updated_at
)
SELECT
  user_id,
  CASE WHEN user_id = 1 THEN 'bench_viewer' ELSE 'bench_user_' || user_id END,
  password_hash.value,
  CASE WHEN user_id = 1 THEN 'Benchmark Viewer' ELSE 'Benchmark User ' || user_id END,
  '',
  'isolated benchmark fixture',
  0,
  1,
  'user',
  1,
  NOW(),
  NOW()
FROM generate_series(1, :benchmark_users) AS users(user_id)
CROSS JOIN password_hash
ON CONFLICT (id) DO NOTHING;

SELECT setval(
  pg_get_serial_sequence('account', 'id'),
  GREATEST((SELECT COALESCE(MAX(id), 1) FROM account), 1),
  true
);

WITH anchor AS MATERIALIZED (
  SELECT date_trunc('second', NOW()) AS published_anchor
)
INSERT INTO video (
  id, author_id, title, description, media_url, cover_url,
  media_asset_id, cover_asset_id, media_status, media_error_code,
  review_version, version, status, visibility, published_at,
  idempotency_key, created_at, updated_at
)
SELECT
  video_id,
  ((video_id - 1) % :benchmark_authors) + 1,
  'Benchmark Video ' || video_id,
  'Synthetic Feed benchmark fixture ' || video_id,
  'https://benchmark.invalid/video/' || video_id || '.mp4',
  'https://benchmark.invalid/cover/' || video_id || '.jpg',
  NULL,
  NULL,
  'legacy_ready',
  '',
  1,
  1,
  2,
  'public',
  anchor.published_anchor - make_interval(secs => ((video_id - 1) / 3)::integer),
  NULL,
  anchor.published_anchor - make_interval(secs => ((video_id - 1) / 3)::integer),
  anchor.published_anchor - make_interval(secs => ((video_id - 1) / 3)::integer)
FROM generate_series(1, :benchmark_videos) AS videos(video_id)
CROSS JOIN anchor
ON CONFLICT (id) DO NOTHING;

SELECT setval(
  pg_get_serial_sequence('video', 'id'),
  GREATEST((SELECT COALESCE(MAX(id), 1) FROM video), 1),
  true
);

INSERT INTO video_stat (
  video_id, like_count, comment_count, favorite_count, created_at, updated_at
)
SELECT
  video_id,
  (video_id * 17 % 10000)::integer,
  (video_id * 7 % 1000)::integer,
  (video_id * 11 % 3000)::integer,
  NOW(),
  NOW()
FROM generate_series(1, :benchmark_videos) AS videos(video_id)
ON CONFLICT (video_id) DO NOTHING;

-- Each user follows a deterministic ring of authors. This creates stable,
-- repeatable long-tail reads without invoking any public API or event worker.
INSERT INTO user_follow (
  user_id, target_user_id, status, idempotency_key, created_at, updated_at
)
SELECT
  user_id,
  ((user_id - 1 + follow_offset) % :benchmark_authors) + 1,
  1,
  NULL,
  NOW(),
  NOW()
FROM generate_series(1, :benchmark_users) AS users(user_id)
CROSS JOIN generate_series(1, :benchmark_follows_per_user) AS offsets(follow_offset)
ON CONFLICT (user_id, target_user_id) DO UPDATE
SET status = 1, updated_at = EXCLUDED.updated_at;

-- Author 2 is the deterministic big creator and always crosses the 10,000
-- follower threshold. Author 3 provides a medium-fanout comparison cohort.
INSERT INTO user_follow (
  user_id, target_user_id, status, idempotency_key, created_at, updated_at
)
SELECT user_id, 2, 1, NULL, NOW(), NOW()
FROM generate_series(1, :benchmark_users) AS users(user_id)
WHERE user_id <> 2
ON CONFLICT (user_id, target_user_id) DO UPDATE
SET status = 1, updated_at = EXCLUDED.updated_at;

INSERT INTO user_follow (
  user_id, target_user_id, status, idempotency_key, created_at, updated_at
)
SELECT user_id, 3, 1, NULL, NOW(), NOW()
FROM generate_series(1, LEAST(:benchmark_users, 5001)) AS users(user_id)
WHERE user_id <> 3
ON CONFLICT (user_id, target_user_id) DO UPDATE
SET status = 1, updated_at = EXCLUDED.updated_at;

WITH following AS (
  SELECT user_id, COUNT(*)::integer AS following_count
  FROM user_follow
  WHERE status = 1
  GROUP BY user_id
), followers AS (
  SELECT target_user_id AS user_id, COUNT(*)::integer AS follower_count
  FROM user_follow
  WHERE status = 1
  GROUP BY target_user_id
)
INSERT INTO user_relation_stat (
  user_id, following_count, follower_count, created_at, updated_at
)
SELECT
  account.id,
  COALESCE(following.following_count, 0),
  COALESCE(followers.follower_count, 0),
  NOW(),
  NOW()
FROM account
LEFT JOIN following ON following.user_id = account.id
LEFT JOIN followers ON followers.user_id = account.id
ON CONFLICT (user_id) DO UPDATE
SET following_count = EXCLUDED.following_count,
    follower_count = EXCLUDED.follower_count,
    updated_at = EXCLUDED.updated_at;

ANALYZE account;
ANALYZE video;
ANALYZE video_stat;
ANALYZE user_follow;
ANALYZE user_relation_stat;

SELECT 'accounts' AS fixture, COUNT(*) AS rows FROM account
UNION ALL
SELECT 'videos', COUNT(*) FROM video
UNION ALL
SELECT 'video_stats', COUNT(*) FROM video_stat
UNION ALL
SELECT 'active_follows', COUNT(*) FROM user_follow WHERE status = 1;

SELECT user_id, following_count, follower_count
FROM user_relation_stat
WHERE user_id IN (1, 2, 3)
ORDER BY user_id;
