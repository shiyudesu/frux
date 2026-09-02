WITH followed AS (
  SELECT f.target_user_id AS author_id, rs.follower_count
  FROM user_follow AS f
  JOIN user_relation_stat AS rs ON rs.user_id = f.target_user_id
  WHERE f.user_id = 1 AND f.status = 1
), inbox_items AS (
  SELECT v.id AS video_id, v.author_id, v.published_at
  FROM video AS v
  JOIN followed ON followed.author_id = v.author_id
  WHERE followed.follower_count < 10000
    AND v.status = 2
    AND v.visibility = 'public'
    AND v.media_status IN ('legacy_ready', 'ready')
    AND v.published_at IS NOT NULL
  ORDER BY v.published_at DESC, v.id DESC
  LIMIT 1000
), ranked_outbox_items AS (
  SELECT
    v.id AS video_id,
    v.author_id,
    v.published_at,
    ROW_NUMBER() OVER (
      PARTITION BY v.author_id
      ORDER BY v.published_at DESC, v.id DESC
    ) AS author_rank
  FROM video AS v
  JOIN followed ON followed.author_id = v.author_id
  WHERE followed.follower_count >= 10000
    AND v.status = 2
    AND v.visibility = 'public'
    AND v.media_status IN ('legacy_ready', 'ready')
    AND v.published_at IS NOT NULL
), index_rows AS (
  SELECT
    'feed:following:inbox:v2:1' AS redis_key,
    video_id,
    author_id,
    published_at
  FROM inbox_items
  UNION ALL
  SELECT
    'feed:following:author:v2:' || author_id AS redis_key,
    video_id,
    author_id,
    published_at
  FROM ranked_outbox_items
  WHERE author_rank <= 500
)
SELECT
  redis_key,
  (
    FLOOR(EXTRACT(EPOCH FROM published_at) * 1000000)::bigint
  )::text AS redis_score,
  LPAD(video_id::text, 20, '0') || ':' ||
  LPAD(author_id::text, 20, '0') || ':' ||
    TO_CHAR(
      published_at AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
    ) AS redis_member
FROM index_rows
ORDER BY redis_key, redis_score DESC;
