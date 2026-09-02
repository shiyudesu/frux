#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const directory = process.argv[2];
if (!directory) {
  throw new Error("usage: summarize-feed-cache.mjs <results-directory>");
}

const patterns = {
  cache: /^feed-timeline-cache-(disabled|sequential|batch)-round(\d+)-first80-vu20-60s-(\d{8}T\d{6}Z)\.json$/,
  short: /^feed-timeline-mget-(sequential|batch)-round(\d+)-first80-vu20-10s-(\d{8}T\d{6}Z)\.json$/,
  page: /^feed-timeline-page(10|50|100)-round(\d+)-first80-vu20-30s-(\d{8}T\d{6}Z)\.json$/,
  burst: /^feed-timeline-(cold|warm)-round(\d+)-first100-vu100-iter100-(\d{8}T\d{6}Z)\.json$/,
};

function select(pattern) {
  const values = [];
  for (const name of fs.readdirSync(directory)) {
    const match = name.match(pattern);
    if (match) {
      values.push({ group: match[1], round: Number(match[2]), timestamp: match[3], name });
    }
  }
  return values.sort((left, right) => left.group.localeCompare(right.group) || left.round - right.round);
}

function read(name) {
  return fs.readFileSync(path.join(directory, name), "utf8");
}

function readJSON(name) {
  return JSON.parse(read(name));
}

function redisCommand(content, command) {
  const prefix = `cmdstat_${command}:`;
  const line = content.split(/\r?\n/).find((value) => value.startsWith(prefix));
  if (!line) {
    return 0;
  }
  const calls = line.slice(prefix.length).split(",").find((value) => value.startsWith("calls="));
  return calls ? Number(calls.slice(6)) : 0;
}

function redisValue(content, key) {
  const prefix = `${key}:`;
  const line = content.split(/\r?\n/).find((value) => value.startsWith(prefix));
  return line ? Number(line.slice(prefix.length)) : 0;
}

function prometheusValue(content, metric, expectedLabels) {
  for (const line of content.split(/\r?\n/)) {
    if (!line.startsWith(`${metric}{`) && !line.startsWith(`${metric} `)) {
      continue;
    }
    const space = line.lastIndexOf(" ");
    if (space < 0) {
      continue;
    }
    const labels = {};
    const open = line.indexOf("{");
    const close = line.indexOf("}");
    if (open >= 0 && close > open) {
      for (const item of line.slice(open + 1, close).split(",")) {
        const [key, raw] = item.split("=", 2);
        labels[key] = raw?.replace(/^"|"$/g, "");
      }
    }
    if (Object.entries(expectedLabels).every(([key, value]) => labels[key] === value)) {
      return Number(line.slice(space + 1));
    }
  }
  return 0;
}

function postgresValues(content) {
  const values = content.trim().split("|").map(Number);
  const keys = ["xact_commit", "xact_rollback", "blks_read", "blks_hit", "tup_returned", "tup_fetched", "temp_files", "temp_bytes", "deadlocks"];
  return Object.fromEntries(keys.map((key, index) => [key, values[index] || 0]));
}

function difference(after, before) {
  return Object.fromEntries(Object.keys(after).map((key) => [key, after[key] - before[key]]));
}

function runData(file) {
  const base = file.name.replace(/\.json$/, "");
  const summary = readJSON(file.name);
  const beforeRedis = read(`${base}-redis-before.txt`);
  const afterRedis = read(`${base}-redis-after.txt`);
  const beforePostgres = postgresValues(read(`${base}-postgres-before.txt`));
  const afterPostgres = postgresValues(read(`${base}-postgres-after.txt`));
  const beforeAPI = read(`${base}-api-before.prom`);
  const afterAPI = read(`${base}-api-after.prom`);
  const redis = {};
  for (const command of ["get", "mget", "set", "hgetall", "del", "unlink"]) {
    redis[command] = redisCommand(afterRedis, command) - redisCommand(beforeRedis, command);
  }
  for (const key of ["total_commands_processed", "total_net_input_bytes", "total_net_output_bytes", "used_memory"]) {
    redis[key] = redisValue(afterRedis, key) - redisValue(beforeRedis, key);
  }
  const cache = {};
  for (const area of ["page", "card", "stat"]) {
    cache[area] = {};
    for (const result of ["hit", "miss", "error"]) {
      const labels = { area, result };
      cache[area][result] = prometheusValue(afterAPI, "frux_feed_cache_requests_total", labels) -
        prometheusValue(beforeAPI, "frux_feed_cache_requests_total", labels);
    }
  }
  return {
    group: file.group,
    round: file.round,
    file: file.name,
    http: {
      requests: summary.metrics.http_reqs.count,
      qps: summary.metrics.http_reqs.rate,
      avg_ms: summary.metrics.http_req_duration.avg,
      median_ms: summary.metrics.http_req_duration.med,
      p95_ms: summary.metrics.http_req_duration["p(95)"],
      p99_ms: summary.metrics.http_req_duration["p(99)"],
      max_ms: summary.metrics.http_req_duration.max,
      first_p99_ms: summary.metrics.feed_first_page_duration?.["p(99)"] || 0,
      cursor_p99_ms: summary.metrics.feed_cursor_page_duration?.["p(99)"] || 0,
      error_rate: summary.metrics.http_req_failed.value,
      duplicate_free_rate: summary.metrics.feed_duplicate_free_rate.value,
      ordered_rate: summary.metrics.feed_ordered_rate.value,
      bytes_received: summary.metrics.data_received.count,
      bytes_sent: summary.metrics.data_sent.count,
    },
    redis,
    postgres: difference(afterPostgres, beforePostgres),
    cache,
  };
}

function median(values) {
  const ordered = [...values].sort((left, right) => left - right);
  const middle = Math.floor(ordered.length / 2);
  return ordered.length % 2 ? ordered[middle] : (ordered[middle - 1] + ordered[middle]) / 2;
}

function round(value, digits = 3) {
  const scale = 10 ** digits;
  return Math.round(value * scale) / scale;
}

function medians(runs) {
  const result = {
    http: {}, redis: {}, postgres: {}, cache: {},
  };
  for (const key of Object.keys(runs[0].http)) {
    result.http[key] = round(median(runs.map((run) => run.http[key])));
  }
  for (const key of Object.keys(runs[0].redis)) {
    result.redis[key] = round(median(runs.map((run) => run.redis[key])));
  }
  for (const key of Object.keys(runs[0].postgres)) {
    result.postgres[key] = round(median(runs.map((run) => run.postgres[key])));
  }
  for (const area of ["page", "card", "stat"]) {
    result.cache[area] = {};
    for (const key of ["hit", "miss", "error"]) {
      result.cache[area][key] = round(median(runs.map((run) => run.cache[area][key])));
    }
  }
	result.redis_per_request = Object.fromEntries(
		Object.keys(result.redis).map((key) => [key, round(median(runs.map((run) => run.redis[key] / run.http.requests)))]),
	);
	result.postgres_per_request = Object.fromEntries(
		Object.keys(result.postgres).map((key) => [key, round(median(runs.map((run) => run.postgres[key] / run.http.requests)))]),
	);
	for (const area of ["page", "card", "stat"]) {
		result.cache[area].hit_rate = round(median(runs.map((run) => {
			const requested = run.cache[area].hit + run.cache[area].miss;
			return requested > 0 ? run.cache[area].hit / requested : 0;
		})), 6);
	}
  return result;
}

function percentChange(before, after) {
  return round((after - before) / before * 100, 2);
}

function percentReduction(before, after) {
  return round((before - after) / before * 100, 2);
}

const files = {
  cache: select(patterns.cache),
  short: select(patterns.short),
  page: select(patterns.page),
  burst: select(patterns.burst),
};
for (const [kind, selected] of Object.entries(files)) {
  const expected = kind === "cache" || kind === "page" ? 9 : 6;
  if (selected.length !== expected) {
    throw new Error(`${kind}: expected ${expected} runs, found ${selected.length}`);
  }
}

const runs = Object.fromEntries(Object.entries(files).map(([kind, selected]) => [kind, selected.map(runData)]));
const summary = {};
for (const [kind, values] of Object.entries(runs)) {
  summary[kind] = {};
  for (const group of [...new Set(values.map((value) => value.group))]) {
    summary[kind][group] = medians(values.filter((value) => value.group === group));
  }
}

const disabled = summary.cache.disabled;
const sequential = summary.cache.sequential;
const batch = summary.cache.batch;
const comparison = {
  batch_vs_disabled_qps_change_percent: percentChange(disabled.http.qps, batch.http.qps),
  batch_vs_disabled_p99_reduction_percent: percentReduction(disabled.http.p99_ms, batch.http.p99_ms),
  batch_vs_disabled_postgres_transactions_per_request_reduction_percent: percentReduction(disabled.postgres_per_request.xact_commit, batch.postgres_per_request.xact_commit),
  batch_vs_disabled_postgres_tuples_fetched_per_request_reduction_percent: percentReduction(disabled.postgres_per_request.tup_fetched, batch.postgres_per_request.tup_fetched),
  batch_vs_sequential_qps_change_percent: percentChange(sequential.http.qps, batch.http.qps),
  batch_vs_sequential_p99_change_percent: percentChange(sequential.http.p99_ms, batch.http.p99_ms),
  batch_vs_sequential_redis_read_command_reduction_percent: percentReduction(
    sequential.redis_per_request.get + sequential.redis_per_request.mget,
    batch.redis_per_request.get + batch.redis_per_request.mget,
  ),
  short_batch_vs_sequential_qps_change_percent: percentChange(summary.short.sequential.http.qps, summary.short.batch.http.qps),
  short_batch_vs_sequential_median_reduction_percent: percentReduction(summary.short.sequential.http.median_ms, summary.short.batch.http.median_ms),
  short_batch_vs_sequential_p99_change_percent: percentChange(summary.short.sequential.http.p99_ms, summary.short.batch.http.p99_ms),
  short_batch_vs_sequential_redis_read_command_reduction_percent: percentReduction(
    summary.short.sequential.redis_per_request.get + summary.short.sequential.redis_per_request.mget,
    summary.short.batch.redis_per_request.get + summary.short.batch.redis_per_request.mget,
  ),
  warm_vs_cold_burst_p99_reduction_percent: percentReduction(summary.burst.cold.http.p99_ms, summary.burst.warm.http.p99_ms),
  warm_vs_cold_burst_qps_change_percent: percentChange(summary.burst.cold.http.qps, summary.burst.warm.http.qps),
};

process.stdout.write(`${JSON.stringify({ files, runs, summary, comparison }, null, 2)}\n`);
