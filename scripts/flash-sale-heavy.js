// flash-sale-heavy.js
//
// 高并发 + 大体量压测 — 测入口层 (Redis Lua + publishMutex + Gin) 的抗击打能力。
//
// 库存故意设得很小 (100),让 99.8% 请求走 out_of_stock 快路径,
// 这样压测完全在测"入口拒绝无效请求"的速度,不被 consumer 串行消化能力限制。
//
// 对比 flash-sale-burst.js: VUs 从 500 升到 2000, iterations 从 1万升到 5万.
//
// 运行前先:
//   1) 清环境 + warmup
//   2) 库存设为 100
//   curl -X POST http://localhost:9090/flash-sale/warmup \
//     -H 'Content-Type: application/json' \
//     -d '{"items":[{"id":"prod_UL9Jg69oRkUThn","quantity":100}],"ttl_seconds":1800}'

import http from 'k6/http';
import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';

const successRT = new Trend('success_rt');
const successCount = new Counter('success_count');
const outOfStock = new Counter('out_of_stock');
const duplicate = new Counter('duplicate');
const notActive = new Counter('not_active');
const serverError = new Counter('server_error');
const businessError = new Counter('business_error');
const unknownError = new Counter('unknown_error');

export const options = {
  scenarios: {
    heavy: {
      executor: 'shared-iterations',
      vus: 2000,              // 4x 的并发量
      iterations: 50000,      // 5x 的总请求
      maxDuration: '3m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(99)<1000'],     // p99 放宽到 1s (2000 VU 下 mutex 排队更深)
    'success_rt': ['p(95)<500'],           // 成功 p95 放宽到 500ms
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(90)', 'p(95)', 'p(99)'],
};

const ITEM_ID = 'prod_UL9Jg69oRkUThn';
const URL = 'http://localhost:9090/flash-sale/orders';

let debugLogged = 0;
const DEBUG_LIMIT = 5;

export default function () {
  const customerId = `heavy-${__VU}-${__ITER}`;

  const payload = JSON.stringify({
    customer_id: customerId,
    items: [{ id: ITEM_ID, quantity: 1 }],
  });

  const res = http.post(URL, payload, {
    headers: { 'Content-Type': 'application/json' },
    timeout: '10s',
  });

  let body = {};
  try { body = JSON.parse(res.body); } catch (e) {}

  const isSuccess = res.status === 200 && body.data && body.data.token;
  const errmsg = (body && body.message) ? String(body.message) : '';
  const errno = (body && typeof body.errno === 'number') ? body.errno : null;

  if (isSuccess) {
    successCount.add(1);
    successRT.add(res.timings.duration);
  } else if (res.status >= 500) {
    serverError.add(1);
    logDebug('5xx', res);
  } else if (errmsg.indexOf('out of stock') !== -1 || errmsg.indexOf('insufficient') !== -1) {
    outOfStock.add(1);
  } else if (errmsg.indexOf('can only place one') !== -1 || errmsg.indexOf('duplicate') !== -1) {
    duplicate.add(1);
  } else if (errmsg.indexOf('not active') !== -1) {
    notActive.add(1);
  } else if (errno !== null && errno !== 0) {
    businessError.add(1);
    logDebug('business', res);
  } else {
    unknownError.add(1);
    logDebug('unknown', res);
  }

  check(res, {
    'no 5xx': (r) => r.status < 500,
    'has response': (r) => r.body && r.body.length > 0,
  });
}

function logDebug(kind, res) {
  if (debugLogged < DEBUG_LIMIT) {
    debugLogged++;
    const body = res.body ? String(res.body).substring(0, 300) : '';
    console.log(`[${kind}] status=${res.status} body=${body}`);
  }
}

function getCount(data, metric) {
  if (!data.metrics[metric]) return 0;
  if (!data.metrics[metric].values) return 0;
  return data.metrics[metric].values.count || 0;
}

function getPercentile(data, metric, p) {
  if (!data.metrics[metric]) return null;
  if (!data.metrics[metric].values) return null;
  const v = data.metrics[metric].values[p];
  return (typeof v === 'number') ? v : null;
}

function fmt(v) {
  return (v === null || v === undefined) ? 'N/A' : v.toFixed(0);
}

export function handleSummary(data) {
  const total = getCount(data, 'iterations');
  const succ = getCount(data, 'success_count');
  const oos = getCount(data, 'out_of_stock');
  const dup = getCount(data, 'duplicate');
  const na = getCount(data, 'not_active');
  const serv = getCount(data, 'server_error');
  const biz = getCount(data, 'business_error');
  const unk = getCount(data, 'unknown_error');

  const dur = data.metrics.iteration_duration ? (data.metrics.iteration_duration.values['avg'] || 0) : 0;
  const rps = total / ((data.state && data.state.testRunDurationMs) ? data.state.testRunDurationMs / 1000 : 1);

  const sum = `
═══════════════════════════════════════════
  Flash Sale HEAVY Test — Summary
═══════════════════════════════════════════
  Total requests:   ${total}
  Throughput:       ${rps.toFixed(0)} req/s

  Success (抢到):    ${succ}
  Out of stock:      ${oos}
  Duplicate:         ${dup}
  Not active:        ${na}
  Business error:    ${biz}
  Server error 5xx:  ${serv}
  Unknown error:     ${unk}

  Sum check: ${succ + oos + dup + na + biz + serv + unk} (应等于 Total=${total})

  Success RT (成功请求):
    p50:  ${fmt(getPercentile(data, 'success_rt', 'p(50)'))} ms
    p90:  ${fmt(getPercentile(data, 'success_rt', 'p(90)'))} ms
    p95:  ${fmt(getPercentile(data, 'success_rt', 'p(95)'))} ms
    p99:  ${fmt(getPercentile(data, 'success_rt', 'p(99)'))} ms

  Overall HTTP RT:
    p50:  ${fmt(getPercentile(data, 'http_req_duration', 'p(50)'))} ms
    p90:  ${fmt(getPercentile(data, 'http_req_duration', 'p(90)'))} ms
    p95:  ${fmt(getPercentile(data, 'http_req_duration', 'p(95)'))} ms
    p99:  ${fmt(getPercentile(data, 'http_req_duration', 'p(99)'))} ms
═══════════════════════════════════════════
`;

  return { 'stdout': sum };
}
