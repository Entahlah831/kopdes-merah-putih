const rupiahFormatter = new Intl.NumberFormat("id-ID", {
  style: "currency",
  currency: "IDR",
  maximumFractionDigits: 0,
  minimumFractionDigits: 0,
});

const jakartaDateFormatter = new Intl.DateTimeFormat("id-ID", {
  timeZone: "Asia/Jakarta",
  year: "numeric",
  month: "short",
  day: "2-digit",
});

const jakartaDateTimeFormatter = new Intl.DateTimeFormat("id-ID", {
  timeZone: "Asia/Jakarta",
  year: "numeric",
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

const jakartaISOFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Jakarta",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

/**
 * Mengubah nilai nominal Rupiah integer (int64) atau string desimal ("600356.00")
 * menjadi tampilan Rupiah standar Indonesia tanpa aritmetika float pecahan.
 */
export function formatRupiah(value: number | string): string {
  const whole = toWholeRupiah(value);
  return rupiahFormatter.format(whole).replace(/\u00a0/g, " ");
}

export function toWholeRupiah(value: number | string): number {
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return 0;
    return Math.trunc(value);
  }
  const trimmed = String(value).trim();
  if (!trimmed) return 0;
  const negative = trimmed.startsWith("-");
  const unsigned = negative ? trimmed.slice(1) : trimmed;
  // Tangani format desimal DB seperti "600356.00" dengan membuang bagian desimal secara deterministik
  const dotIdx = unsigned.indexOf(".");
  const intPart = dotIdx >= 0 ? unsigned.slice(0, dotIdx) : unsigned;
  const digits = intPart.replace(/[^0-9]/g, "");
  if (!digits) return 0;
  const parsed = Number.parseInt(digits, 10);
  if (!Number.isFinite(parsed)) return 0;
  return negative ? -parsed : parsed;
}

/**
 * Mengubah input teks seperti "Rp 25.000" menjadi integer bulat 25000.
 */
export function parseRupiahInput(input: string): number {
  const digits = String(input ?? "").replace(/[^0-9]/g, "");
  if (!digits) return 0;
  const n = Number.parseInt(digits, 10);
  return Number.isFinite(n) ? n : 0;
}

export function formatJakartaDate(input: string | number | Date): string {
  const d = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(d.getTime())) return "-";
  return jakartaDateFormatter.format(d);
}

export function formatJakartaDateTime(input: string | number | Date): string {
  const d = input instanceof Date ? input : new Date(input);
  if (Number.isNaN(d.getTime())) return "-";
  return `${jakartaDateTimeFormatter.format(d)} WIB`;
}

/**
 * Menghasilkan tanggal hari ini format YYYY-MM-DD dalam zona waktu Asia/Jakarta.
 */
export function getJakartaTodayISO(nowMs: number = Date.now()): string {
  return jakartaISOFormatter.format(new Date(nowMs));
}

/**
 * Menyamarkan NIK 16 digit menjadi 3201********0001 untuk tampilan UI aman PDP.
 */
export function maskNIK(nik: string): string {
  const digits = String(nik ?? "").replace(/\D/g, "");
  if (digits.length >= 8) {
    return `${digits.slice(0, 4)}${"*".repeat(digits.length - 8)}${digits.slice(-4)}`;
  }
  return "*".repeat(Math.max(4, digits.length || 16));
}
