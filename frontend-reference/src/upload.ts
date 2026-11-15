export interface FileLike {
  name: string;
  size: number;
  type: string;
}

export interface UploadValidationResult {
  ok: boolean;
  reason?: string;
}

export function validateUploadFile(
  file: FileLike,
  opts: { maxBytes: number; allowedMimeTypes: string[] }
): UploadValidationResult {
  if (!file || !file.name) {
    return { ok: false, reason: "Berkas wajib dipilih terlebih dahulu." };
  }
  if (file.size <= 0) {
    return { ok: false, reason: "Berkas kosong (0 byte)." };
  }
  if (file.size > opts.maxBytes) {
    const maxMB = (opts.maxBytes / (1024 * 1024)).toFixed(1);
    return { ok: false, reason: `Ukuran berkas melebihi batas maksimal ${maxMB} MB.` };
  }
  if (opts.allowedMimeTypes.length > 0 && !opts.allowedMimeTypes.includes(file.type)) {
    return {
      ok: false,
      reason: `Tipe berkas '${file.type || "tidak dikenal"}' tidak didukung. Gunakan: ${opts.allowedMimeTypes.join(", ")}.`,
    };
  }
  return { ok: true };
}

/**
 * Mengompres foto KYC di sisi browser memakai HTMLCanvasElement bila berjalan di DOM.
 * Pada environment Node headless / berkas non-gambar, mengembalikan file apa adanya.
 */
export async function compressImageInBrowser(
  file: File,
  opts: { maxWidth?: number; quality?: number; maxBytes?: number } = {}
): Promise<File> {
  if (typeof document === "undefined" || typeof createImageBitmap === "undefined") {
    return file;
  }
  if (!file.type.startsWith("image/")) {
    return file;
  }
  const maxWidth = opts.maxWidth ?? 1600;
  const quality = opts.quality ?? 0.8;
  const bitmap = await createImageBitmap(file);
  try {
    const scale = bitmap.width > maxWidth ? maxWidth / bitmap.width : 1;
    const targetW = Math.max(1, Math.round(bitmap.width * scale));
    const targetH = Math.max(1, Math.round(bitmap.height * scale));
    const canvas = document.createElement("canvas");
    canvas.width = targetW;
    canvas.height = targetH;
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.drawImage(bitmap, 0, 0, targetW, targetH);

    const blob = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob((b) => resolve(b), "image/jpeg", quality)
    );
    if (!blob) return file;
    if (opts.maxBytes && blob.size > opts.maxBytes && blob.size >= file.size) {
      return file;
    }
    const baseName = file.name.replace(/\.[^.]+$/, "");
    return new File([blob], `${baseName}.jpg`, { type: "image/jpeg" });
  } finally {
    bitmap.close();
  }
}

export async function uploadMultipartWithProgress(
  url: string,
  formData: FormData,
  opts: {
    accessToken?: string | null;
    onProgress?: (percent: number) => void;
    signal?: AbortSignal;
    fetchImpl?: typeof fetch;
  } = {}
): Promise<Response> {
  // Gunakan XMLHttpRequest hanya di browser bila caller ingin progress bar upload nyata
  if (typeof XMLHttpRequest !== "undefined" && opts.onProgress) {
    return new Promise<Response>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", url, true);
      if (opts.accessToken) {
        xhr.setRequestHeader("Authorization", `Bearer ${opts.accessToken}`);
      }
      const onAbort = () => xhr.abort();
      if (opts.signal) {
        if (opts.signal.aborted) {
          reject(new DOMException("Aborted", "AbortError"));
          return;
        }
        opts.signal.addEventListener("abort", onAbort, { once: true });
      }
      xhr.upload.onprogress = (ev) => {
        if (ev.lengthComputable && ev.total > 0) {
          opts.onProgress?.(Math.round((ev.loaded / ev.total) * 100));
        }
      };
      xhr.onload = () => {
        opts.signal?.removeEventListener("abort", onAbort);
        const headers = new Headers();
        const reqId = xhr.getResponseHeader("X-Request-ID");
        if (reqId) headers.set("X-Request-ID", reqId);
        const ct = xhr.getResponseHeader("Content-Type");
        if (ct) headers.set("Content-Type", ct);
        resolve(new Response(xhr.responseText, { status: xhr.status, headers }));
      };
      xhr.onerror = () => {
        opts.signal?.removeEventListener("abort", onAbort);
        reject(new Error("Gagal mengunggah berkas ke server."));
      };
      xhr.onabort = () => {
        opts.signal?.removeEventListener("abort", onAbort);
        reject(new DOMException("Aborted", "AbortError"));
      };
      xhr.send(formData);
    });
  }

  const headers = new Headers();
  if (opts.accessToken) {
    headers.set("Authorization", `Bearer ${opts.accessToken}`);
  }
  const fetcher = opts.fetchImpl ?? globalThis.fetch;
  return fetcher(url, {
    method: "POST",
    headers,
    body: formData,
    signal: opts.signal,
  });
}
