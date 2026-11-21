export interface ProcessedImage {
  blob: Blob;
  filename: string;
  width: number;
  height: number;
}

const ALLOWED_TYPES = new Set(["image/jpeg", "image/png"]);
const MAX_INPUT_BYTES = 8 * 1024 * 1024;
const MAX_DIMENSION = 4096;
const TARGET_MAX_EDGE = 1280;

export async function validateAndCompressImage(file: File): Promise<ProcessedImage> {
  if (!ALLOWED_TYPES.has(file.type)) {
    throw new Error("Format berkas ditolak: hanya JPG atau PNG yang diizinkan (PDF tidak didukung).");
  }
  if (file.size <= 0 || file.size > MAX_INPUT_BYTES) {
    throw new Error("Ukuran berkas harus antara 1 byte hingga 8 MB.");
  }

  const objectURL = URL.createObjectURL(file);
  try {
    const img = await loadHTMLImage(objectURL);
    const width = img.naturalWidth;
    const height = img.naturalHeight;
    if (width <= 0 || height <= 0 || width > MAX_DIMENSION || height > MAX_DIMENSION) {
      throw new Error("Dimensi gambar tidak valid atau melebihi batas 4096x4096 piksel.");
    }

    const scale = Math.min(1, TARGET_MAX_EDGE / Math.max(width, height));
    const targetW = Math.max(1, Math.round(width * scale));
    const targetH = Math.max(1, Math.round(height * scale));

    const canvas = document.createElement("canvas");
    canvas.width = targetW;
    canvas.height = targetH;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      throw new Error("Browser gagal menyiapkan kanvas kompresi gambar.");
    }
    ctx.drawImage(img, 0, 0, targetW, targetH);

    const outType = file.type === "image/png" ? "image/png" : "image/jpeg";
    const blob = await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        (res) => {
          if (!res) {
            reject(new Error("Gagal mengonversi gambar melalui kanvas."));
            return;
          }
          resolve(res);
        },
        outType,
        0.85
      );
    });

    const ext = outType === "image/png" ? "png" : "jpg";
    const base = (file.name || "dokumen").replace(/\.[^.]+$/, "").replace(/[^a-zA-Z0-9_-]/g, "_");
    return {
      blob,
      filename: `${base || "dokumen"}.${ext}`,
      width: targetW,
      height: targetH
    };
  } finally {
    URL.revokeObjectURL(objectURL);
  }
}

function loadHTMLImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("Berkas bukan gambar JPG/PNG yang valid atau rusak."));
    img.src = src;
  });
}
