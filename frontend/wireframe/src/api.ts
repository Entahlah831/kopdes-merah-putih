import { PublicConfigData, RuntimeConfigJSON } from "./types";
import { isTerminalStatus } from "./dom";

export interface APIEnvelope<T = unknown> {
  success: boolean;
  data: T;
  message?: string;
  error?: {
    code?: string;
    message?: string;
  };
  request_id?: string;
}

export class APIClient {
  private runtimeCfg: RuntimeConfigJSON | null = null;
  private publicCfg: PublicConfigData | null = null;

  async init(): Promise<{ runtime: RuntimeConfigJSON; publicCfg: PublicConfigData }> {
    const rtRes = await fetch("/config.json", { cache: "no-store" });
    if (!rtRes.ok) {
      throw new Error(`Gagal memuat /config.json (${rtRes.status})`);
    }
    this.runtimeCfg = (await rtRes.json()) as RuntimeConfigJSON;
    if (typeof this.runtimeCfg?.api_public_url !== "string") {
      throw new Error("api_public_url tidak valid pada /config.json");
    }
    const pubEnv = await this.request<PublicConfigData>("GET", "/api/v1/public/config");
    this.publicCfg = pubEnv.data;
    return { runtime: this.runtimeCfg, publicCfg: this.publicCfg };
  }

  getRuntime(): RuntimeConfigJSON | null {
    return this.runtimeCfg;
  }

  getPublic(): PublicConfigData | null {
    return this.publicCfg;
  }

  getAPIBase(): string {
    return (this.runtimeCfg?.api_public_url || "").replace(/\/+$/, "");
  }

  newIdempotencyKey(prefix = "wf"): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  }

  async request<T = unknown>(
    method: string,
    path: string,
    options?: {
      body?: unknown;
      formData?: FormData;
      headers?: Record<string, string>;
      signal?: AbortSignal;
    }
  ): Promise<APIEnvelope<T>> {
    const url = `${this.getAPIBase()}${path}`;
    const headers: Record<string, string> = {
      "X-Requested-With": "XMLHttpRequest",
      ...(options?.headers || {})
    };
    let reqBody: BodyInit | undefined;
    if (options?.formData) {
      reqBody = options.formData;
    } else if (options?.body !== undefined) {
      headers["Content-Type"] = "application/json";
      reqBody = JSON.stringify(options.body);
    }

    const res = await fetch(url, {
      method,
      credentials: "include",
      headers,
      body: reqBody,
      signal: options?.signal
    });

    const text = await res.text();
    let parsed: APIEnvelope<T>;
    try {
      parsed = JSON.parse(text) as APIEnvelope<T>;
    } catch {
      throw new Error(`Respons server bukan JSON (HTTP ${res.status}): ${text.slice(0, 120)}`);
    }
    if (!res.ok || parsed.success === false) {
      const code = parsed.error?.code ? `[${parsed.error.code}] ` : "";
      const msg = parsed.error?.message || parsed.message || `HTTP ${res.status}`;
      const err = new Error(`${code}${msg}`) as Error & { status?: number; code?: string };
      err.status = res.status;
      err.code = parsed.error?.code;
      throw err;
    }
    return parsed;
  }

  async fetchBlob(path: string, signal?: AbortSignal): Promise<{ url: string; contentType: string }> {
    const url = `${this.getAPIBase()}${path}`;
    const res = await fetch(url, {
      method: "GET",
      credentials: "include",
      headers: { "X-Requested-With": "XMLHttpRequest" },
      signal
    });
    if (!res.ok) {
      throw new Error(`Gagal memuat berkas terenkripsi (HTTP ${res.status})`);
    }
    const blob = await res.blob();
    return {
      url: URL.createObjectURL(blob),
      contentType: res.headers.get("Content-Type") || blob.type || "image/jpeg"
    };
  }

  async fetchText(path: string, signal?: AbortSignal): Promise<string> {
    const url = `${this.getAPIBase()}${path}`;
    const res = await fetch(url, {
      method: "GET",
      credentials: "include",
      headers: { "X-Requested-With": "XMLHttpRequest" },
      signal
    });
    if (!res.ok) {
      throw new Error(`Gagal memuat data teks/CSV (HTTP ${res.status})`);
    }
    return await res.text();
  }
}

export function startTerminalPoll<T>(
  fetchOnce: (signal: AbortSignal) => Promise<T>,
  extractStatus: (data: T) => string,
  onTick: (data: T, stopped: boolean) => void,
  intervalMs = 3000,
  maxTicks = 10
): () => void {
  const controller = new AbortController();
  let ticks = 0;
  let timer: number | undefined;

  const step = async (): Promise<void> => {
    if (controller.signal.aborted) return;
    ticks += 1;
    try {
      const data = await fetchOnce(controller.signal);
      const st = extractStatus(data);
      const terminal = isTerminalStatus(st) || ticks >= maxTicks;
      onTick(data, terminal);
      if (terminal) {
        controller.abort();
        return;
      }
      timer = window.setTimeout(() => {
        void step();
      }, intervalMs);
    } catch {
      if (!controller.signal.aborted && ticks < maxTicks) {
        timer = window.setTimeout(() => {
          void step();
        }, intervalMs);
      }
    }
  };

  timer = window.setTimeout(() => {
    void step();
  }, intervalMs);

  return () => {
    controller.abort();
    if (timer !== undefined) {
      window.clearTimeout(timer);
    }
  };
}
