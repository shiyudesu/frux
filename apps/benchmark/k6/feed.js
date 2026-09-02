import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

const BASE_URL = (__ENV.BASE_URL || "http://api:8080").replace(/\/$/, "");
const SCENE = (__ENV.SCENE || "timeline").toLowerCase();
const LIMIT = Number(__ENV.LIMIT || 20);
const THINK_TIME = Number(__ENV.THINK_TIME || 0);
const ACCOUNT = __ENV.ACCOUNT || "bench_viewer";
const PASSWORD = __ENV.PASSWORD || "";
const ITERATIONS = Number(__ENV.ITERATIONS || 0);
const FIRST_PAGE_PERCENT = Number(__ENV.FIRST_PAGE_PERCENT || 80);

if (![20, 80, 100].includes(FIRST_PAGE_PERCENT)) {
  throw new Error("FIRST_PAGE_PERCENT must be 20, 80, or 100");
}

const successRate = new Rate("feed_success_rate");
const duplicateFreeRate = new Rate("feed_duplicate_free_rate");
const orderedRate = new Rate("feed_ordered_rate");
const firstPageDuration = new Trend("feed_first_page_duration", true);
const cursorPageDuration = new Trend("feed_cursor_page_duration", true);
const returnedItems = new Trend("feed_returned_items");

let cursor = "";
let requestID = "";
let seenVideoIDs = {};
let previousLastItem = null;

const executionOptions = ITERATIONS > 0
  ? { vus: Number(__ENV.VUS || 20), iterations: ITERATIONS }
  : { vus: Number(__ENV.VUS || 20), duration: __ENV.DURATION || "60s" };

export const options = {
  ...executionOptions,
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
  thresholds: {
    http_req_failed: ["rate<0.01"],
    feed_success_rate: ["rate>0.99"],
  },
};

export function setup() {
  if (SCENE !== "following" && SCENE !== "recommend") {
    return { token: "" };
  }
  if (!PASSWORD) {
    throw new Error("PASSWORD is required for following and recommend scenes");
  }
  const response = http.post(
    `${BASE_URL}/api/sessions`,
    JSON.stringify({ account: ACCOUNT, password: PASSWORD }),
    { headers: { "Content-Type": "application/json" }, tags: { operation: "login" } },
  );
  if (response.status !== 200) {
    throw new Error(`benchmark login failed: status=${response.status} body=${response.body}`);
  }
  return { token: response.json("access_token") };
}

function resetPaginationState() {
  cursor = "";
  requestID = "";
  seenVideoIDs = {};
  previousLastItem = null;
}

function sortableTimestamp(value) {
  if (typeof value !== "string" || !value.endsWith("Z")) {
    return "";
  }
  const content = value.slice(0, -1);
  const dot = content.indexOf(".");
  if (dot < 0) {
    return `${content}.000000000Z`;
  }
  const whole = content.slice(0, dot);
  const fraction = content.slice(dot + 1);
  if (!/^\d{1,9}$/.test(fraction)) {
    return "";
  }
  return `${whole}.${fraction.padEnd(9, "0")}Z`;
}

function itemComesAfterOrEqual(previous, current) {
  if (!previous || !current) {
    return true;
  }
  const previousTime = sortableTimestamp(previous.published_at);
  const currentTime = sortableTimestamp(current.published_at);
  if (!previousTime || !currentTime) {
    return false;
  }
  if (currentTime < previousTime) {
    return true;
  }
  if (currentTime > previousTime) {
    return false;
  }
  return Number(current.video_id) <= Number(previous.video_id);
}

function observeCorrectness(items) {
  let duplicateFree = true;
  let ordered = true;
  let previous = previousLastItem;
  for (const item of items) {
    const videoID = String(item.video_id);
    if (seenVideoIDs[videoID]) {
      duplicateFree = false;
    }
    seenVideoIDs[videoID] = true;
    if (!itemComesAfterOrEqual(previous, item)) {
      ordered = false;
    }
    previous = item;
  }
  if (items.length > 0) {
    previousLastItem = items[items.length - 1];
  }
  duplicateFreeRate.add(duplicateFree);
  orderedRate.add(ordered);
}

function readPage(response, firstPage) {
  if (firstPage) {
    firstPageDuration.add(response.timings.duration);
  } else {
    cursorPageDuration.add(response.timings.duration);
  }
  let body;
  try {
    body = response.json();
  } catch (_) {
    body = null;
  }
  const ok = check(response, {
    "feed status is 200": (value) => value.status === 200,
    "feed response has items": () => body !== null && Array.isArray(body.items),
  });
  successRate.add(ok);
  if (!ok || !body) {
    resetPaginationState();
    return;
  }
  returnedItems.add(body.items.length);
  observeCorrectness(body.items);
  cursor = body.has_more && body.next_cursor ? body.next_cursor : "";
  if (!cursor) {
    requestID = "";
  }
}

function getFeed(token) {
  const firstPage = !cursor;
  let url = `${BASE_URL}/api/feed-items?scene=${encodeURIComponent(SCENE)}&limit=${LIMIT}`;
  if (cursor) {
    url += `&cursor=${encodeURIComponent(cursor)}`;
  }
  const headers = token ? { Authorization: `Bearer ${token}` } : {};
  const response = http.get(url, {
    headers,
    tags: { operation: `feed_${SCENE}` },
  });
  readPage(response, firstPage);
}

function getRecommendation(token) {
  const firstPage = !cursor;
  if (!requestID) {
    requestID = `benchmark-${__VU}-${__ITER}-${Date.now()}`;
  }
  const response = http.post(
    `${BASE_URL}/api/feed-queries`,
    JSON.stringify({
      scene: "recommend",
      cursor,
      limit: LIMIT,
      context: {
        request_id: requestID,
        network_class: "unknown",
        viewport_class: "unknown",
      },
    }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      tags: { operation: "feed_recommend" },
    },
  );
  readPage(response, firstPage);
}

function shouldResetForTrafficMix(iteration) {
  if (FIRST_PAGE_PERCENT === 100) {
    return true;
  }
  if (FIRST_PAGE_PERCENT === 80) {
    return iteration % 5 !== 4;
  }
  return iteration % 5 === 0;
}

export default function (data) {
  // The deterministic mix avoids random run-to-run drift. At 80%, four
  // requests start a new session and the fifth reads the next cursor page.
  if (shouldResetForTrafficMix(__ITER) || !cursor) {
    resetPaginationState();
  }

  if (SCENE === "recommend") {
    getRecommendation(data.token);
  } else {
    getFeed(data.token);
  }

  if (THINK_TIME > 0) {
    sleep(THINK_TIME);
  }
}
