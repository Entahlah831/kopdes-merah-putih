export interface PaymentPollerOptions<T> {
  intervalMs?: number;
  minIntervalOverrideMs?: number;
  expiresAtMs?: number;
  clockSkewMs?: number;
  fetchStatus: (signal: AbortSignal) => Promise<T>;
  onUpdate: (data: T) => void;
  onError?: (err: unknown) => void;
  onExpire?: () => void;
  isTerminal?: (data: T) => boolean;
}

export interface PaymentPollerHandle {
  start: () => void;
  stop: () => void;
  isRunning: () => boolean;
}

const DEFAULT_TERMINAL_STATUSES = new Set([
  "disetujui",
  "ditolak",
  "kedaluwarsa",
  "batal",
  "lunas",
  "selesai",
]);

/**
 * Polling status pembayaran yang hemat kuota desa:
 * - Otomatis menjeda saat tab tersembunyi (document.visibilityState === "hidden")
 * - Otomatis berhenti pada status terminal atau melewati batas kedaluwarsa (kompensasi clockSkewMs)
 * - Menggunakan AbortController untuk membatalkan request gantung saat .stop()
 */
export function createPaymentPoller<T extends { status?: string }>(
  opts: PaymentPollerOptions<T>
): PaymentPollerHandle {
  const minClamp = opts.minIntervalOverrideMs ?? 2000;
  const intervalMs = Math.max(minClamp, opts.intervalMs ?? 3000);
  const skew = opts.clockSkewMs ?? 0;

  let running = false;
  let timerId: ReturnType<typeof setTimeout> | null = null;
  let activeController: AbortController | null = null;

  const isTerminal =
    opts.isTerminal ??
    ((d: T) => Boolean(d?.status && DEFAULT_TERMINAL_STATUSES.has(d.status)));

  const clearScheduled = () => {
    if (timerId !== null) {
      clearTimeout(timerId);
      timerId = null;
    }
  };

  const stop = () => {
    running = false;
    clearScheduled();
    if (activeController) {
      activeController.abort();
      activeController = null;
    }
    if (typeof document !== "undefined" && document.removeEventListener) {
      document.removeEventListener("visibilitychange", onVisibilityChange);
    }
  };

  const isExpired = (): boolean => {
    if (!opts.expiresAtMs) return false;
    return Date.now() + skew >= opts.expiresAtMs;
  };

  const tick = async () => {
    if (!running) return;
    if (typeof document !== "undefined" && document.visibilityState === "hidden") {
      return;
    }
    if (isExpired()) {
      stop();
      opts.onExpire?.();
      return;
    }

    activeController = new AbortController();
    try {
      const data = await opts.fetchStatus(activeController.signal);
      if (!running) return;
      opts.onUpdate(data);
      if (isTerminal(data)) {
        stop();
        return;
      }
    } catch (err) {
      if (!running) return;
      if ((err as { name?: string })?.name !== "AbortError") {
        opts.onError?.(err);
      }
    } finally {
      activeController = null;
    }

    if (running) {
      timerId = setTimeout(tick, intervalMs);
    }
  };

  const onVisibilityChange = () => {
    if (!running) return;
    if (document.visibilityState === "hidden") {
      clearScheduled();
    } else {
      clearScheduled();
      void tick();
    }
  };

  const start = () => {
    if (running) return;
    running = true;
    if (typeof document !== "undefined" && document.addEventListener) {
      document.addEventListener("visibilitychange", onVisibilityChange);
    }
    void tick();
  };

  return {
    start,
    stop,
    isRunning: () => running,
  };
}
