// Mixed Wireframe Load/Stress/Spike/Soak Scenario for k6
// Run against local Set A / Set B stack only (NEVER against third-party hosts).
// Example:
//   k6 run --env API_BASE_URL=http://127.0.0.1:${API_HOST_PORT} \
//          --env TEST_USER_EMAIL=anggota1@kopdes-test.local \
//          --env TEST_USER_PASSWORD=${TEST_SEED_PASSWORD} \
//          --env SCENARIO=load tests/perf/k6_mixed.js
import http from 'k6/http';
import { check, group, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const API_BASE = (__ENV.API_BASE_URL || '').replace(/\/+$/, '');
if (!API_BASE) {
  throw new Error('API_BASE_URL wajib disetel dari env (dilarang hardcode port)');
}

const READ_P95_MS = Number(__ENV.SLO_READ_P95_MS || 300);
const WRITE_P95_MS = Number(__ENV.SLO_WRITE_P95_MS || 800);
const MAX_ERR = Number(__ENV.SLO_MAX_ERROR_RATE || 0.005);
const SCENARIO = (__ENV.SCENARIO || 'load').toLowerCase();

const readDur = new Trend('kopdes_read_duration', true);
const writeDur = new Trend('kopdes_write_duration', true);
const errRate = new Rate('kopdes_unexpected_error');

function scenarioConfig() {
  switch (SCENARIO) {
    case 'smoke':
      return { executor: 'constant-vus', vus: 5, duration: '15s' };
    case 'stress':
      return {
        executor: 'ramping-vus',
        startVUs: 25,
        stages: [
          { duration: '30s', target: 100 },
          { duration: '30s', target: 250 },
          { duration: '30s', target: 500 },
          { duration: '15s', target: 0 },
        ],
      };
    case 'spike':
      return {
        executor: 'ramping-vus',
        startVUs: 10,
        stages: [
          { duration: '10s', target: 10 },
          { duration: '15s', target: 250 },
          { duration: '20s', target: 250 },
          { duration: '15s', target: 10 },
        ],
      };
    case 'soak':
      return { executor: 'constant-vus', vus: 50, duration: __ENV.SOAK_DURATION || '2h' };
    case 'load':
    default:
      return { executor: 'constant-vus', vus: 100, duration: '60s' };
  }
}

export const options = {
  scenarios: {
    mixed_wireframe: scenarioConfig(),
  },
  thresholds: {
    kopdes_read_duration: [`p(95)<${READ_P95_MS}`],
    kopdes_write_duration: [`p(95)<${WRITE_P95_MS}`],
    kopdes_unexpected_error: [`rate<${MAX_ERR}`],
  },
};

export function setup() {
  const email = __ENV.TEST_USER_EMAIL || 'anggota1@kopdes-test.local';
  const password = __ENV.TEST_USER_PASSWORD || '';
  if (!password) {
    throw new Error('TEST_USER_PASSWORD wajib disetel dari env');
  }
  const res = http.post(
    `${API_BASE}/api/v1/auth/login`,
    JSON.stringify({ identifier: email, password }),
    { headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'k6' } }
  );
  check(res, { 'login ok': (r) => r.status === 200 });
  const body = res.json();
  const token = (body && body.data && body.data.session_token) || '';

  const catRes = http.get(`${API_BASE}/api/v1/katalog/produk`);
  let produkID = '';
  if (catRes.status === 200) {
    const items = (catRes.json() || {}).data || [];
    for (const p of items) {
      if (p.stok_tersedia > 0 && p.harga_aktif_tersedia) {
        produkID = p.id;
        break;
      }
    }
  }
  return { token, produkID };
}

export default function (data) {
  const ip = `10.10.${__VU % 250}.${(__ITER % 250) + 1}`;
  const headers = {
    'X-Forwarded-For': ip,
    'X-Requested-With': 'k6',
    'Content-Type': 'application/json',
  };
  if (data.token) {
    headers.Authorization = `Bearer ${data.token}`;
  }

  group('public_and_catalog_reads', () => {
    const r1 = http.get(`${API_BASE}/api/v1/public/config`, { headers });
    readDur.add(r1.timings.duration);
    errRate.add(r1.status >= 500 || r1.status === 0);

    const r2 = http.get(`${API_BASE}/api/v1/katalog/produk`, { headers });
    readDur.add(r2.timings.duration);
    errRate.add(r2.status >= 500 || r2.status === 0);
  });

  group('member_portal_reads', () => {
    const r3 = http.get(`${API_BASE}/api/v1/iuran/tagihan`, { headers });
    readDur.add(r3.timings.duration);
    errRate.add(r3.status >= 500 || r3.status === 0);

    const r4 = http.get(`${API_BASE}/api/v1/pesanan`, { headers });
    readDur.add(r4.timings.duration);
    errRate.add(r4.status >= 500 || r4.status === 0);

    const r5 = http.get(`${API_BASE}/api/v1/pembayaran`, { headers });
    readDur.add(r5.timings.duration);
    errRate.add(r5.status >= 500 || r5.status === 0);
  });

  if (data.produkID && __ITER % 5 === 0) {
    group('checkout_and_cancel_write', () => {
      const idem = `k6-${__VU}-${__ITER}-${Date.now()}`;
      const w1 = http.post(
        `${API_BASE}/api/v1/toko/checkout`,
        JSON.stringify({
          items: [{ produk_id: data.produkID, kuantitas: 1 }],
          metode_pengiriman: 'ambil_sendiri',
          metode_pembayaran: 'qris',
          nama_penerima: 'Load Test',
          hp_penerima: '08110000004',
        }),
        { headers: { ...headers, 'Idempotency-Key': idem } }
      );
      writeDur.add(w1.timings.duration);
      errRate.add(w1.status >= 500 || w1.status === 0);
      if (w1.status === 201) {
        const oid = w1.json('data.id');
        if (oid) {
          const w2 = http.patch(
            `${API_BASE}/api/v1/pesanan/${oid}/status`,
            JSON.stringify({ status: 'batal', catatan: 'k6 cleanup' }),
            { headers }
          );
          writeDur.add(w2.timings.duration);
        }
      }
    });
  }
  sleep(0.2);
}
