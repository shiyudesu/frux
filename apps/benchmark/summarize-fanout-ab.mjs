#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const resultsDirectory = process.argv[2];
if (!resultsDirectory) {
  throw new Error("usage: summarize-fanout-ab.mjs <results-directory>");
}

const fanoutPattern = /^kafka-fanout-(all_push|hybrid)-round(\d+)-100-events-(\d{8}T\d{6}Z)\.json$/;
const feedPattern = /^feed-following-(all_push|hybrid)-round(\d+)-first80-vu20-60s-(\d{8}T\d{6}Z)\.json$/;

function latestFiles(pattern) {
  const selected = new Map();
  for (const name of fs.readdirSync(resultsDirectory)) {
    const match = name.match(pattern);
    if (!match) {
      continue;
    }
    const key = `${match[1]}:${match[2]}`;
    const previous = selected.get(key);
    if (!previous || match[3] > previous.timestamp) {
      selected.set(key, { strategy: match[1], round: Number(match[2]), timestamp: match[3], name });
    }
  }
  return [...selected.values()].sort((left, right) => left.round - right.round || left.strategy.localeCompare(right.strategy));
}

function readJSON(name) {
  return JSON.parse(fs.readFileSync(path.join(resultsDirectory, name), "utf8"));
}

function redisInfoValue(content, key) {
  const prefix = `${key}:`;
  const line = content.split(/\r?\n/).find((value) => value.startsWith(prefix));
  return line ? Number(line.slice(prefix.length)) : 0;
}

function redisCommandCalls(content, command) {
  const prefix = `cmdstat_${command}:`;
  const line = content.split(/\r?\n/).find((value) => value.startsWith(prefix));
  if (!line) {
    return 0;
  }
  const calls = line.slice(prefix.length).split(",").find((value) => value.startsWith("calls="));
  return calls ? Number(calls.slice("calls=".length)) : 0;
}

function feedRedisDelta(feedFile) {
  const base = feedFile.replace(/\.json$/, "");
  const before = fs.readFileSync(path.join(resultsDirectory, `${base}-redis-before.txt`), "utf8");
  const after = fs.readFileSync(path.join(resultsDirectory, `${base}-redis-after.txt`), "utf8");
  const command = (name) => redisCommandCalls(after, name) - redisCommandCalls(before, name);
  return {
    zcard: command("zcard"),
    zrevrangebyscore: command("zrevrangebyscore"),
    mget: command("mget"),
    total_commands: redisInfoValue(after, "total_commands_processed") - redisInfoValue(before, "total_commands_processed"),
    net_input_bytes: redisInfoValue(after, "total_net_input_bytes") - redisInfoValue(before, "total_net_input_bytes"),
    net_output_bytes: redisInfoValue(after, "total_net_output_bytes") - redisInfoValue(before, "total_net_output_bytes"),
  };
}

function median(values) {
  const ordered = [...values].sort((left, right) => left - right);
  const middle = Math.floor(ordered.length / 2);
  return ordered.length % 2 === 0 ? (ordered[middle - 1] + ordered[middle]) / 2 : ordered[middle];
}

function rounded(value, digits = 2) {
  const scale = 10 ** digits;
  return Math.round(value * scale) / scale;
}

function percentChange(before, after) {
  return rounded((after - before) / before * 100);
}

function percentReduction(before, after) {
  return rounded((before - after) / before * 100);
}

const fanoutFiles = latestFiles(fanoutPattern);
const feedFiles = latestFiles(feedPattern);
for (const strategy of ["all_push", "hybrid"]) {
  if (fanoutFiles.filter((item) => item.strategy === strategy).length < 3 || feedFiles.filter((item) => item.strategy === strategy).length < 3) {
    throw new Error(`missing three complete ${strategy} rounds`);
  }
}

const fanout = {};
const feed = {};
for (const strategy of ["all_push", "hybrid"]) {
  const fanoutRuns = fanoutFiles.filter((item) => item.strategy === strategy).map((item) => readJSON(item.name));
  const selectedFeedFiles = feedFiles.filter((item) => item.strategy === strategy);
  const feedRuns = selectedFeedFiles.map((item) => readJSON(item.name));
  const feedRedisRuns = selectedFeedFiles.map((item) => feedRedisDelta(item.name));
  fanout[strategy] = {
    producer_events_per_second: rounded(median(fanoutRuns.map((run) => run.events_per_second))),
    producer_ack_p99_ms: rounded(median(fanoutRuns.map((run) => run.producer_acknowledgement.p99_ms))),
    max_consumer_lag: median(fanoutRuns.map((run) => run.max_consumer_lag)),
    recovery_ms: rounded(median(fanoutRuns.map((run) => run.recovery_ms))),
    visibility_p50_ms: rounded(median(fanoutRuns.map((run) => run.index_visibility.p50_ms))),
    visibility_p95_ms: rounded(median(fanoutRuns.map((run) => run.index_visibility.p95_ms))),
    visibility_p99_ms: rounded(median(fanoutRuns.map((run) => run.index_visibility.p99_ms))),
    logical_index_entries: median(fanoutRuns.map((run) => run.following_index.total_entries)),
    redis_following_write_commands: median(fanoutRuns.map((run) => run.redis_commands.following_index_write_commands)),
    redis_zadd_commands: median(fanoutRuns.map((run) => run.redis_commands.zadd)),
    redis_memory_delta_bytes: median(fanoutRuns.map((run) => run.redis_commands.used_memory_delta_bytes)),
    worker_average_ms: rounded(median(fanoutRuns.map((run) => run.fanout_worker.average_ms))),
    worker_p95_upper_ms: median(fanoutRuns.map((run) => run.fanout_worker.p95_upper_bound_ms)),
    worker_success_each_round: fanoutRuns.map((run) => run.fanout_worker.success),
    worker_failure_total: fanoutRuns.reduce((sum, run) => sum + run.fanout_worker.failure, 0),
    missing_visibility_total: fanoutRuns.reduce((sum, run) => sum + run.missing_visibility, 0),
    exact_index_every_round: fanoutRuns.every((run) => run.following_index.exact),
  };
  feed[strategy] = {
    qps: rounded(median(feedRuns.map((run) => run.metrics.http_reqs.rate))),
    p95_ms: rounded(median(feedRuns.map((run) => run.metrics.http_req_duration["p(95)"]))),
    p99_ms: rounded(median(feedRuns.map((run) => run.metrics.http_req_duration["p(99)"]))),
    first_page_p99_ms: rounded(median(feedRuns.map((run) => run.metrics.feed_first_page_duration["p(99)"]))),
    cursor_page_p99_ms: rounded(median(feedRuns.map((run) => run.metrics.feed_cursor_page_duration["p(99)"]))),
    error_rate: median(feedRuns.map((run) => run.metrics.http_req_failed.value)),
    duplicate_free_rate: median(feedRuns.map((run) => run.metrics.feed_duplicate_free_rate.value)),
    ordered_rate: median(feedRuns.map((run) => run.metrics.feed_ordered_rate.value)),
    redis_zcard_per_request: rounded(median(feedRedisRuns.map((run, index) => run.zcard / feedRuns[index].metrics.http_reqs.count)), 3),
    redis_zrevrangebyscore_per_request: rounded(median(feedRedisRuns.map((run, index) => run.zrevrangebyscore / feedRuns[index].metrics.http_reqs.count)), 3),
    redis_mget_per_request: rounded(median(feedRedisRuns.map((run, index) => run.mget / feedRuns[index].metrics.http_reqs.count)), 3),
    redis_total_commands_per_request: rounded(median(feedRedisRuns.map((run, index) => run.total_commands / feedRuns[index].metrics.http_reqs.count)), 3),
  };
}

const comparison = {
  logical_index_entry_reduction_percent: percentReduction(fanout.all_push.logical_index_entries, fanout.hybrid.logical_index_entries),
  redis_following_write_command_reduction_percent: percentReduction(fanout.all_push.redis_following_write_commands, fanout.hybrid.redis_following_write_commands),
  redis_zadd_reduction_percent: percentReduction(fanout.all_push.redis_zadd_commands, fanout.hybrid.redis_zadd_commands),
  redis_memory_reduction_percent: percentReduction(fanout.all_push.redis_memory_delta_bytes, fanout.hybrid.redis_memory_delta_bytes),
  lag_recovery_reduction_percent: percentReduction(fanout.all_push.recovery_ms, fanout.hybrid.recovery_ms),
  visibility_p95_reduction_percent: percentReduction(fanout.all_push.visibility_p95_ms, fanout.hybrid.visibility_p95_ms),
  visibility_p99_reduction_percent: percentReduction(fanout.all_push.visibility_p99_ms, fanout.hybrid.visibility_p99_ms),
  worker_average_reduction_percent: percentReduction(fanout.all_push.worker_average_ms, fanout.hybrid.worker_average_ms),
  read_qps_change_percent: percentChange(feed.all_push.qps, feed.hybrid.qps),
  read_p99_change_percent: percentChange(feed.all_push.p99_ms, feed.hybrid.p99_ms),
  first_page_p99_change_percent: percentChange(feed.all_push.first_page_p99_ms, feed.hybrid.first_page_p99_ms),
  cursor_page_p99_change_percent: percentChange(feed.all_push.cursor_page_p99_ms, feed.hybrid.cursor_page_p99_ms),
  read_redis_range_commands_change_percent: percentChange(feed.all_push.redis_zrevrangebyscore_per_request, feed.hybrid.redis_zrevrangebyscore_per_request),
  read_redis_total_commands_change_percent: percentChange(feed.all_push.redis_total_commands_per_request, feed.hybrid.redis_total_commands_per_request),
};

process.stdout.write(`${JSON.stringify({ fanout_files: fanoutFiles, feed_files: feedFiles, fanout, feed, comparison }, null, 2)}\n`);
