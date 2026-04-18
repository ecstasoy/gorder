// flash-sale-ramp.js
//
// 逐步加压测试 — 从 200 QPS 一路升到 5000 QPS,找出系统的崩溃点。
// 用 ramping-arrival-rate executor 按固定速率注入请求,不依赖 VU 数量。
//
// 观察要点:
// - 哪个 QPS 水位开始出现 http_req_failed
// - 哪个 QPS 水位 p95 RT 突然飙升 (通常是系统进入过载)
// - 哪个 QPS 水位出现 business_error / 5xx
//
// 运行前 warmup 100 件 (小库存让 99%+ 的请求走 Lua 拒绝快路径,测入口极限)
//   curl -X POST http://localhost:9090/flash-sale/warmup \
//     -H 'Content-Type: application/json' \
//     -d '{"items":[{"id":"prod_UL9Jg69oRkUThn","quantity":100}],"ttl_seconds":1800}'

import http from 'k6/http';
import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';

const successRT = new Trend('success_rt');
const successCount = new Counter('success_count');
const outOfStock = new Counter('out_of_stock');
const businessError = new Counter('business_error');
const serverError = new Counter('server_error');

export const options = {
  scenarios: {
    ramp: {
      executor: 'ramping-arrival-rate',
      startRate: 200,                // 初始 200 QPS
      timeUnit: '1s',
      preAllocatedVUs: 500,
      maxVUs: 3000,
      stages: [
        { duration: '20s', target: 500 },    // 200 → 500
        { duration: '30s', target: 1000 },   // 500 → 1000
        { duration: '30s', target: 2000 },   // 1000 → 2000
        { duration: '30s', target: 3000 },   // 2000 → 3000
        { duration: '30s', target: 5000 },   // 3000 → 5000 (预期崩溃点)
        { duration: '20s', target: 0 },      // cool down
      ],
    },
  },
  thresholds: {
    // 不设硬阈值,目的是找崩溃点而不是 pass/fail
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(90)', 'p(95)', 'p(99)'],
};

const ITEM_ID = 'prod_UL9Jg69oRkUThn';
const URL = 'http://localhost:9090/flash-sale/orders';

let debugLogged = 0;
const DEBUG_LIMIT = 10;

export default function () {
  // 加时间戳避免高 QPS 下 __ITER 在同一秒重复
  const customerId = `ramp-${__VU}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

  const res = http.post(URL, JSON.stringify({
    customer_id: customerId,
    items: [{ id: ITEM_ID, quantity: 1 }],
  }), {
    headers: { 'Content-Type': 'application/json' },
    timeout: '5s',
  });

  let body = {};
  try { body = JSON.parse(res.body); } catch (e) {}

  const isSuccess = res.status === 200 && body.data && body.data.token;
  const errmsg = (body && body.message) ? String(body.message) : '';

  if (isSuccess) {
    successCount.add(1);
    successRT.add(res.timings.duration);
  } else if (res.status >= 500) {
    serverError.add(1);
    logDebug('5xx', res);
  } else if (errmsg.indexOf('out of stock') !== -1 || errmsg.indexOf('insufficient') !== -1) {
    outOfStock.add(1);
  } else {
    businessError.add(1);
    logDebug('business', res);
  }

  check(res, { 'no 5xx': (r) => r.status < 500 });
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
  const biz = getCount(data, 'business_error');
  const serv = getCount(data, 'server_error');

  const sum = `
═══════════════════════════════════════════
  Flash Sale RAMP Test — Summary
═══════════════════════════════════════════
  Total requests:     ${total}
  Success:            ${succ}
  Out of stock:       ${oos}
  Business error:     ${biz}
  Server error 5xx:   ${serv}

  Success RT:
    p50:  ${fmt(getPercentile(data, 'success_rt', 'p(50)'))} ms
    p90:  ${fmt(getPercentile(data, 'success_rt', 'p(90)'))} ms
    p95:  ${fmt(getPercentile(data, 'success_rt', 'p(95)'))} ms
    p99:  ${fmt(getPercentile(data, 'success_rt', 'p(99)'))} ms

  Overall HTTP RT (所有请求):
    p50:  ${fmt(getPercentile(data, 'http_req_duration', 'p(50)'))} ms
    p90:  ${fmt(getPercentile(data, 'http_req_duration', 'p(90)'))} ms
    p95:  ${fmt(getPercentile(data, 'http_req_duration', 'p(95)'))} ms
    p99:  ${fmt(getPercentile(data, 'http_req_duration', 'p(99)'))} ms

  HTTP failure rate:  ${(data.metrics.http_req_failed.values.rate * 100).toFixed(2)}%
═══════════════════════════════════════════

  关注:
  - Success/Business error 比例 → 系统开始吐错误说明饱和了
  - p99 RT 突然变大 → 过载拐点
  - 跑完后对照 k6 实时输出的 "req" 列看在哪个 QPS 水位开始掉
`;

  return { 'stdout': sum };
}
