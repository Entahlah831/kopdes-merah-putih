// Mixed Wireframe Load/Stress/Spike/Soak Scenario for k6 (Per-Endpoint p50/p95/p99)
// Run against local Set A / Set B stack only (NEVER against third-party hosts).
import http from 'k6/http';
import exec from 'k6/execution';
import { check, group, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const API_BASE = (__ENV.API_BASE_URL || '').replace(/\/+$/, '');
if (!API_BASE) {
  throw new Error('API_BASE_URL wajib disetel dari env (dilarang hardcode port)');
}

const READ_P95_MS = Number(__ENV.SLO_READ_P95_MS || 300);
const WRITE_P95_MS = Number(__ENV.SLO_WRITE_P95_MS || 800);
const MAX_ERR = Number(__ENV.SLO_MAX_ERROR_RATE || 0.005);
const SCENARIO = (__ENV.SCENARIO || 'load').toLowerCase();
const READ_VUS = Number(__ENV.K6_LOAD_READ_VUS || 180);
const WRITE_VUS = Number(__ENV.K6_LOAD_WRITE_VUS || 20);
const LOAD_HOLD = __ENV.K6_LOAD_DURATION || '35s';

const readDur = new Trend('kopdes_read_duration', true);
const writeDur = new Trend('kopdes_write_duration', true);
const readReqs = new Counter('kopdes_read_reqs');
const writeReqs = new Counter('kopdes_write_reqs');
const errRate = new Rate('kopdes_unexpected_error');

// 10 Trend metrik per-endpoint (6 baca + 4 tulis penuh alur transaksi)
const epPublicConfig = new Trend('ep_public_config', true);
const epKatalogProduk = new Trend('ep_katalog_produk', true);
const epIuranTagihan = new Trend('ep_iuran_tagihan', true);
const epPesananList = new Trend('ep_pesanan_list', true);
const epPembayaranList = new Trend('ep_pembayaran_list', true);
const epLaporan = new Trend('ep_laporan', true);

const epCheckout = new Trend('ep_checkout', true);
const epPembayaranCreate = new Trend('ep_pembayaran_create', true);
const epBuktiUpload = new Trend('ep_bukti_upload', true);
const epPembayaranVerify = new Trend('ep_pembayaran_verify', true);

const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let c = i;
    for (let k = 0; k < 8; k++) {
      c = (c & 1) !== 0 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    t[i] = c >>> 0;
  }
  return t;
})();

function crc32Bytes(bytes) {
  let c = 0xffffffff;
  for (let i = 0; i < bytes.length; i++) {
    c = CRC_TABLE[(c ^ bytes[i]) & 0xff] ^ (c >>> 8);
  }
  return (c ^ 0xffffffff) >>> 0;
}

const PNG_SIG = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
const PNG_IHDR = [
  0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
  0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
  0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde,
];
const PNG_IDAT = [
  0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54,
  0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00,
  0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92, 0xef,
];
const PNG_IEND = [0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82];

// Menghasilkan gambar PNG 1x1 valid ber-chunk tEXt unik agar setiap unggah lolos ValidateImage & anti-duplikat SHA-256.
function makeUniquePNG(tag) {
  const key = 'Comment\0' + tag;
  const textLen = key.length;
  const typeAndData = new Uint8Array(4 + textLen);
  typeAndData[0] = 0x74; // t
  typeAndData[1] = 0x45; // E
  typeAndData[2] = 0x58; // X
  typeAndData[3] = 0x74; // t
  for (let i = 0; i < textLen; i++) {
    typeAndData[4 + i] = key.charCodeAt(i) & 0x7f;
  }
  const crc = crc32Bytes(typeAndData);
  const totalLen = PNG_SIG.length + PNG_IHDR.length + (8 + textLen + 4) + PNG_IDAT.length + PNG_IEND.length;
  const out = new Uint8Array(totalLen);
  let pos = 0;
  out.set(PNG_SIG, pos); pos += PNG_SIG.length;
  out.set(PNG_IHDR, pos); pos += PNG_IHDR.length;
  out[pos++] = (textLen >>> 24) & 0xff;
  out[pos++] = (textLen >>> 16) & 0xff;
  out[pos++] = (textLen >>> 8) & 0xff;
  out[pos++] = textLen & 0xff;
  out.set(typeAndData, pos); pos += typeAndData.length;
  out[pos++] = (crc >>> 24) & 0xff;
  out[pos++] = (crc >>> 16) & 0xff;
  out[pos++] = (crc >>> 8) & 0xff;
  out[pos++] = crc & 0xff;
  out.set(PNG_IDAT, pos); pos += PNG_IDAT.length;
  out.set(PNG_IEND, pos);
  return out.buffer;
}

function buildScenarios() {
  switch (SCENARIO) {
    case 'smoke':
      return {
        browsing_and_reports: { executor: 'constant-vus', exec: 'runRead', vus: 5, duration: '15s' },
        transactional_writes: { executor: 'constant-vus', exec: 'runWrite', vus: 2, duration: '15s' },
      };
    case 'stress':
      return {
        browsing_and_reports: {
          executor: 'ramping-vus',
          exec: 'runRead',
          startVUs: 25,
          stages: [
            { duration: '25s', target: 150 },
            { duration: '25s', target: 300 },
            { duration: '25s', target: 450 },
            { duration: '10s', target: 0 },
          ],
        },
        transactional_writes: {
          executor: 'constant-vus',
          exec: 'runWrite',
          vus: WRITE_VUS,
          duration: '85s',
        },
      };
    case 'spike':
      return {
        browsing_and_reports: {
          executor: 'ramping-vus',
          exec: 'runRead',
          startVUs: 10,
          stages: [
            { duration: '10s', target: 20 },
            { duration: '15s', target: 230 },
            { duration: '20s', target: 230 },
            { duration: '10s', target: 20 },
          ],
        },
        transactional_writes: {
          executor: 'constant-vus',
          exec: 'runWrite',
          vus: WRITE_VUS,
          duration: '55s',
        },
      };
    case 'soak':
      return {
        browsing_and_reports: { executor: 'constant-vus', exec: 'runRead', vus: 45, duration: __ENV.SOAK_DURATION || '2h' },
        transactional_writes: { executor: 'constant-vus', exec: 'runWrite', vus: 5, duration: __ENV.SOAK_DURATION || '2h' },
      };
    case 'load':
    default:
      return {
        browsing_and_reports: {
          executor: 'ramping-vus',
          exec: 'runRead',
          startVUs: 20,
          stages: [
            { duration: '10s', target: Math.max(10, Math.round(READ_VUS * 0.56)) },
            { duration: LOAD_HOLD, target: READ_VUS },
            { duration: '5s', target: 0 },
          ],
        },
        transactional_writes: {
          executor: 'ramping-vus',
          exec: 'runWrite',
          startVUs: 2,
          stages: [
            { duration: '10s', target: Math.max(2, Math.round(WRITE_VUS * 0.5)) },
            { duration: LOAD_HOLD, target: WRITE_VUS },
            { duration: '5s', target: 0 },
          ],
        },
      };
  }
}

export const options = {
  setupTimeout: '60s',
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  scenarios: buildScenarios(),
  thresholds: {
    kopdes_read_duration: [`p(95)<${READ_P95_MS}`],
    kopdes_write_duration: [`p(95)<${WRITE_P95_MS}`],
    kopdes_unexpected_error: [`rate<${MAX_ERR}`],
  },
};

function authHeaders(token, ip, contentType = 'application/json') {
  const h = {
    'X-Forwarded-For': ip,
    'X-Requested-With': 'k6',
    Cookie: `kopdes_session=${token}`,
  };
  if (contentType) {
    h['Content-Type'] = contentType;
  }
  return h;
}

function loginAccount(identifier, password, ip) {
  const res = http.post(
    `${API_BASE}/api/v1/auth/login`,
    JSON.stringify({ identifier, password }),
    { headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'k6', 'X-Forwarded-For': ip } }
  );
  check(res, { [`setup login ${identifier} ok`]: (r) => r.status === 200 });
  if (res.status !== 200) {
    throw new Error(`Login gagal untuk ${identifier}: status=${res.status} body=${res.body}`);
  }
  const body = res.json() || {};
  const tok =
    (res.cookies && res.cookies.kopdes_session && res.cookies.kopdes_session[0] && res.cookies.kopdes_session[0].value) ||
    (res.cookies && res.cookies['__Host-kopdes_session'] && res.cookies['__Host-kopdes_session'][0] && res.cookies['__Host-kopdes_session'][0].value) ||
    (body.data && body.data.session_token) ||
    '';
  if (!tok) {
    throw new Error(`Token/cookie sesi kosong untuk ${identifier}`);
  }
  return tok;
}

function ensureLoadProduct(adminToken) {
  const hdrs = authHeaders(adminToken, '10.30.0.10');
  const findRes = http.get(`${API_BASE}/api/v1/katalog/produk?q=K6-LOAD-ITEM`, { headers: hdrs });
  if (findRes.status === 200) {
    const rows = (findRes.json() || {}).data || [];
    for (const r of rows) {
      if (r.sku === 'K6-LOAD-ITEM') {
        return r.id;
      }
    }
  }
  const crRes = http.post(
    `${API_BASE}/api/v1/katalog/produk`,
    JSON.stringify({
      sku: 'K6-LOAD-ITEM',
      nama: 'Paket Uji Beban K6',
      deskripsi: 'Produk sintetis khusus benchmark k6',
      satuan: 'pack',
      harga_umum: 10000,
      harga_anggota: 10000,
      stok: 100000,
      status: 'aktif',
    }),
    { headers: hdrs }
  );
  if (crRes.status !== 201) {
    throw new Error(`Gagal membuat produk K6-LOAD-ITEM: status=${crRes.status} body=${crRes.body}`);
  }
  return crRes.json('data.id');
}

function ensureBuyerSessions(adminToken, count) {
  const hdrs = authHeaders(adminToken, '10.30.0.11');
  const buyerPass = __ENV.K6_BUYER_PASSWORD || 'kopdes_k6_buyer_pass_123!';
  const tokens = [];
  for (let i = 1; i <= count; i++) {
    const idx = String(i).padStart(2, '0');
    const email = `k6buyer${idx}@kopdes-test.local`;
    const hp = `081900000${idx}`;
    const uRes = http.post(
      `${API_BASE}/api/v1/users`,
      JSON.stringify({
        email,
        no_hp: hp,
        nama_lengkap: `K6 Pembeli ${idx}`,
        peran: 'anggota',
        password: buyerPass,
      }),
      { headers: hdrs }
    );
    if (uRes.status !== 201 && uRes.status !== 409) {
      throw new Error(`Gagal membuat akun ${email}: status=${uRes.status} body=${uRes.body}`);
    }
    const tok = loginAccount(email, buyerPass, `10.30.1.${i}`);
    tokens.push(tok);
  }
  return tokens;
}

export function setup() {
  const seedPwd = __ENV.TEST_USER_PASSWORD || '';
  const anggotaToken = __ENV.K6_ANGGOTA_TOKEN || (seedPwd ? loginAccount(__ENV.TEST_USER_EMAIL || 'anggota1@kopdes-test.local', seedPwd, '10.30.0.1') : '');
  const adminToken = __ENV.K6_ADMIN_TOKEN || '';
  const bendaharaToken = __ENV.K6_BENDAHARA_TOKEN || '';
  if (!anggotaToken || !adminToken || !bendaharaToken) {
    throw new Error('K6_ANGGOTA_TOKEN, K6_ADMIN_TOKEN, dan K6_BENDAHARA_TOKEN wajib disediakan dari auth_helper.py');
  }
  const produkID = ensureLoadProduct(adminToken);
  const buyerCount = Math.max(WRITE_VUS + 10, 30);
  const buyers = ensureBuyerSessions(adminToken, buyerCount);
  return { anggotaToken, bendaharaToken, produkID, buyers };
}

function recordRead(metric, res) {
  metric.add(res.timings.duration);
  readDur.add(res.timings.duration);
  readReqs.add(1);
  errRate.add(res.status !== 200);
}

export function runRead(data) {
  const vu = exec.vu.idInTest;
  const it = exec.scenario.iterationInTest;
  const ip = `10.10.${1 + (vu % 240)}.${(it % 250) + 1}`;
  const memberHdrs = authHeaders(data.anggotaToken, ip);
  const reportTypes = ['anggota', 'tunggakan', 'penjualan', 'pembayaran'];
  const reportKind = reportTypes[it % reportTypes.length];
  const staffHdrs = authHeaders(data.bendaharaToken, `10.11.${1 + (vu % 240)}.${(it % 250) + 1}`);

  group('read_wireframe_and_reports', () => {
    recordRead(epPublicConfig, http.get(`${API_BASE}/api/v1/public/config`, { headers: memberHdrs }));
    recordRead(epKatalogProduk, http.get(`${API_BASE}/api/v1/katalog/produk`, { headers: memberHdrs }));
    recordRead(epIuranTagihan, http.get(`${API_BASE}/api/v1/iuran/tagihan`, { headers: memberHdrs }));
    recordRead(epPesananList, http.get(`${API_BASE}/api/v1/pesanan`, { headers: memberHdrs }));
    recordRead(epPembayaranList, http.get(`${API_BASE}/api/v1/pembayaran`, { headers: memberHdrs }));
    recordRead(epLaporan, http.get(`${API_BASE}/api/v1/laporan?jenis=${reportKind}`, { headers: staffHdrs }));
  });
  sleep(2.5);
}

export function runWrite(data) {
  const it = exec.scenario.iterationInInstance;
  const slot = it % data.buyers.length;
  const buyerToken = data.buyers[slot];
  const buyerIP = `10.20.${1 + slot}.${(it % 250) + 1}`;
  const staffIP = `10.21.${1 + slot}.${(it % 250) + 1}`;
  const bHdrs = authHeaders(buyerToken, buyerIP);
  const sHdrs = authHeaders(data.bendaharaToken, staffIP);

  group('write_full_order_payment_verify', () => {
    const qty = (slot % 5) + 1;
    const idemOrd = `k6-${SCENARIO}-ord-${slot}-${it}-${Date.now()}`;
    const w1 = http.post(
      `${API_BASE}/api/v1/toko/checkout`,
      JSON.stringify({
        items: [{ produk_id: data.produkID, kuantitas: qty }],
        metode_pengiriman: 'ambil_sendiri',
        metode_pembayaran: 'qris',
        nama_penerima: `K6 Pembeli ${slot + 1}`,
        hp_penerima: '08190000999',
      }),
      { headers: { ...bHdrs, 'Idempotency-Key': idemOrd } }
    );
    epCheckout.add(w1.timings.duration);
    writeDur.add(w1.timings.duration);
    writeReqs.add(1);
    const ordOK = w1.status === 201;
    errRate.add(!ordOK);
    if (!ordOK) {
      if (it < 3) console.error(`k6 checkout fail it=${it} slot=${slot} status=${w1.status} body=${w1.body}`);
      return;
    }

    const pesananID = w1.json('data.id');
    const idemPay = `k6-${SCENARIO}-pay-${slot}-${it}-${Date.now()}`;
    const w2 = http.post(
      `${API_BASE}/api/v1/pembayaran`,
      JSON.stringify({
        jenis_transaksi: 'pesanan',
        metode: 'qris',
        pesanan_id: pesananID,
        catatan_pembayar: 'k6 checkout',
      }),
      { headers: { ...bHdrs, 'Idempotency-Key': idemPay } }
    );
    epPembayaranCreate.add(w2.timings.duration);
    writeDur.add(w2.timings.duration);
    writeReqs.add(1);
    const payOK = w2.status === 201;
    errRate.add(!payOK);
    if (!payOK) {
      if (it < 3) console.error(`k6 pay create fail it=${it} slot=${slot} status=${w2.status} body=${w2.body}`);
      return;
    }

    const bayarID = w2.json('data.pembayaran.id');
    const nominalTotal = w2.json('data.pembayaran.nominal_total');

    const pngBuf = makeUniquePNG(`${SCENARIO}-${slot}-${it}-${Date.now()}`);
    const upHdrs = authHeaders(buyerToken, buyerIP, null);
    const w3 = http.post(
      `${API_BASE}/api/v1/pembayaran`,
      {
        pembayaran_id: bayarID,
        catatan_pembayar: 'k6 bukti',
        file: http.file(pngBuf, 'bukti.png', 'image/png'),
      },
      { headers: upHdrs }
    );
    epBuktiUpload.add(w3.timings.duration);
    writeDur.add(w3.timings.duration);
    writeReqs.add(1);
    const upOK = w3.status === 200;
    errRate.add(!upOK);
    if (!upOK) {
      if (it < 3) console.error(`k6 bukti upload fail it=${it} slot=${slot} status=${w3.status} body=${w3.body}`);
      return;
    }

    const w4 = http.post(
      `${API_BASE}/api/v1/pembayaran/${bayarID}/verifikasi`,
      JSON.stringify({
        keputusan: 'setuju',
        nominal_dibayar: nominalTotal,
        alasan: 'verifikasi otomatis beban k6',
      }),
      { headers: sHdrs }
    );
    epPembayaranVerify.add(w4.timings.duration);
    writeDur.add(w4.timings.duration);
    writeReqs.add(1);
    const verOK = w4.status === 200;
    errRate.add(!verOK);
    if (!verOK && it < 3) {
      console.error(`k6 verify fail it=${it} slot=${slot} status=${w4.status} body=${w4.body}`);
    }
  });
  sleep(0.8);
}

export default function (data) {
  runRead(data);
}
