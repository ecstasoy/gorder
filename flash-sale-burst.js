// flash-sale-burst.js
import http from 'k6/http';
import { check } from 'k6';
import { Trend, Counter } from 'k6/metrics';

// 自定义指标
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
    burst: {
      executor: 'shared-iterations',
      vus: 500,              // 500 个并发 VU
      iterations: 10000,     // 总共发 1 万个请求
      maxDuration: '2m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(99)<500'],
    'success_rt': ['p(95)<200'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(90)', 'p(95)', 'p(99)'],
};

// Flash SKU (独立 product_id,方案 B);要先调 /flash-sale/warmup 给它充库存
const ITEM_ID = 'prod_UL9Jg69oRkUThn';
const URL = 'http://localhost:9090/flash-sale/orders';

// 用一个小的 shared state 控制 debug 日志的数量(避免刷屏)
let debugLogged = 0;
const DEBUG_LIMIT = 5;

export default function () {
  // 每个请求一个独立 customer_id,避免一人一单互相干扰
  const customerId = `load-test-${__VU}-${__ITER}`;

  const payload = JSON.stringify({
    customer_id: customerId,
    items: [{ id: ITEM_ID, quantity: 1 }],
  });

  const res = http.post(URL, payload, {
    headers: { 'Content-Type': 'application/json' },
    timeout: '10s',
  });

  // 解析业务响应
  let body = {};
  try {
    body = JSON.parse(res.body);
  } catch (e) {
    // 非 JSON 响应
  }

  const isSuccess =
    res.status === 200 &&
    body.data &&
    body.data.token;

  // 错误消息字段统一读,gorder 用的是 message
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
    // 业务层已识别但我们没归类的错误
    businessError.add(1);
    logDebug('business', res);
  } else {
    // 完全无法识别的响应
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

// --- 工具函数:安全读取嵌套属性,避免 Goja 的 optional chaining 兼容问题 ---
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

  const srp50 = getPercentile(data, 'success_rt', 'p(50)');
  const srp95 = getPercentile(data, 'success_rt', 'p(95)');
  const srp99 = getPercentile(data, 'success_rt', 'p(99)');

  const rp50 = getPercentile(data, 'http_req_duration', 'p(50)');
  const rp95 = getPercentile(data, 'http_req_duration', 'p(95)');
  const rp99 = getPercentile(data, 'http_req_duration', 'p(99)');

  const sum = `
═══════════════════════════════════════════
  Flash Sale Burst Test — Summary
═══════════════════════════════════════════
  Total requests:   ${total}

  Success (抢到):    ${succ}
  Out of stock:      ${oos}
  Duplicate:         ${dup}
  Not active:        ${na}
  Business error:    ${biz}
  Server error 5xx:  ${serv}
  Unknown error:     ${unk}

  Sum check: ${succ + oos + dup + na + biz + serv + unk} (应等于 Total=${total})

  Success RT (成功请求):
    p50:  ${fmt(srp50)} ms
    p95:  ${fmt(srp95)} ms
    p99:  ${fmt(srp99)} ms

  Overall HTTP RT:
    p50:  ${fmt(rp50)} ms
    p95:  ${fmt(rp95)} ms
    p99:  ${fmt(rp99)} ms
═══════════════════════════════════════════
`;

  return { 'stdout': sum };
}
