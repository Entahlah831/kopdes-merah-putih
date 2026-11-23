// Generator QR Code ISO/IEC 18004 (Byte Mode, Error Correction Level L, Versi 1-9)
// 100% sisi klien (<canvas>) tanpa dependensi eksternal dan tanpa mengirim rahasia TOTP ke pihak ketiga.

interface QRVersionSpec {
  version: number;
  totalData: number;
  ecPerBlock: number;
  blocks: number;
  align: number[];
  verInfo: number;
}

const QR_SPECS_L: QRVersionSpec[] = [
  { version: 1, totalData: 19, ecPerBlock: 7, blocks: 1, align: [], verInfo: 0 },
  { version: 2, totalData: 34, ecPerBlock: 10, blocks: 1, align: [6, 18], verInfo: 0 },
  { version: 3, totalData: 55, ecPerBlock: 15, blocks: 1, align: [6, 22], verInfo: 0 },
  { version: 4, totalData: 80, ecPerBlock: 20, blocks: 1, align: [6, 26], verInfo: 0 },
  { version: 5, totalData: 108, ecPerBlock: 26, blocks: 1, align: [6, 30], verInfo: 0 },
  { version: 6, totalData: 136, ecPerBlock: 18, blocks: 2, align: [6, 34], verInfo: 0 },
  { version: 7, totalData: 156, ecPerBlock: 20, blocks: 2, align: [6, 22, 38], verInfo: 0x07c94 },
  { version: 8, totalData: 194, ecPerBlock: 24, blocks: 2, align: [6, 24, 42], verInfo: 0x085bc },
  { version: 9, totalData: 232, ecPerBlock: 30, blocks: 2, align: [6, 26, 46], verInfo: 0x09a99 }
];

const GF_EXP = new Uint8Array(512);
const GF_LOG = new Uint8Array(256);
(() => {
  let x = 1;
  for (let i = 0; i < 255; i++) {
    GF_EXP[i] = x;
    GF_LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (let i = 255; i < 512; i++) {
    GF_EXP[i] = GF_EXP[i - 255];
  }
})();

function gfMul(a: number, b: number): number {
  if (a === 0 || b === 0) return 0;
  return GF_EXP[GF_LOG[a] + GF_LOG[b]];
}

function rsComputeEC(data: number[], ecLen: number): number[] {
  let gen = [1];
  for (let i = 0; i < ecLen; i++) {
    const next = new Array<number>(gen.length + 1).fill(0);
    for (let j = 0; j < gen.length; j++) {
      next[j] ^= gen[j];
      next[j + 1] ^= gfMul(gen[j], GF_EXP[i]);
    }
    gen = next;
  }
  const rem = new Array<number>(ecLen).fill(0);
  for (const byte of data) {
    const factor = byte ^ rem[0];
    rem.shift();
    rem.push(0);
    for (let i = 0; i < ecLen; i++) {
      rem[i] ^= gfMul(gen[i + 1], factor);
    }
  }
  return rem;
}

function buildCodewords(bytes: Uint8Array, spec: QRVersionSpec): number[] {
  const bits: number[] = [];
  const pushBits = (val: number, len: number): void => {
    for (let i = len - 1; i >= 0; i--) {
      bits.push((val >> i) & 1);
    }
  };
  pushBits(0b0100, 4);
  pushBits(bytes.length, 8);
  for (const b of bytes) pushBits(b, 8);

  const maxBits = spec.totalData * 8;
  for (let i = 0; i < 4 && bits.length < maxBits; i++) bits.push(0);
  while (bits.length % 8 !== 0) bits.push(0);

  const dataWords: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let w = 0;
    for (let j = 0; j < 8; j++) w = (w << 1) | bits[i + j];
    dataWords.push(w);
  }
  const pads = [0xec, 0x11];
  let pIdx = 0;
  while (dataWords.length < spec.totalData) {
    dataWords.push(pads[pIdx % 2]);
    pIdx++;
  }

  const blockSize = spec.totalData / spec.blocks;
  const dataBlocks: number[][] = [];
  const ecBlocks: number[][] = [];
  for (let b = 0; b < spec.blocks; b++) {
    const blk = dataWords.slice(b * blockSize, (b + 1) * blockSize);
    dataBlocks.push(blk);
    ecBlocks.push(rsComputeEC(blk, spec.ecPerBlock));
  }

  const out: number[] = [];
  for (let i = 0; i < blockSize; i++) {
    for (let b = 0; b < spec.blocks; b++) out.push(dataBlocks[b][i]);
  }
  for (let i = 0; i < spec.ecPerBlock; i++) {
    for (let b = 0; b < spec.blocks; b++) out.push(ecBlocks[b][i]);
  }
  return out;
}

export function encodeQRMatrix(text: string): boolean[][] {
  const bytes = new TextEncoder().encode(text);
  const spec = QR_SPECS_L.find((s) => bytes.length + 2 <= s.totalData);
  if (!spec) {
    throw new Error("URI TOTP terlalu panjang untuk QR Code");
  }

  const size = 17 + 4 * spec.version;
  const modules: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const reserved: boolean[][] = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));

  const setFixed = (r: number, c: number, dark: boolean): void => {
    if (r < 0 || r >= size || c < 0 || c >= size) return;
    modules[r][c] = dark;
    reserved[r][c] = true;
  };

  const placeFinder = (top: number, left: number): void => {
    for (let dr = -1; dr <= 7; dr++) {
      for (let dc = -1; dc <= 7; dc++) {
        const inOuter = dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6;
        const onBorder = dr === 0 || dr === 6 || dc === 0 || dc === 6;
        const inCore = dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4;
        setFixed(top + dr, left + dc, inOuter && (onBorder || inCore));
      }
    }
  };

  placeFinder(0, 0);
  placeFinder(0, size - 7);
  placeFinder(size - 7, 0);

  for (const r of spec.align) {
    for (const c of spec.align) {
      if ((r <= 8 && c <= 8) || (r <= 8 && c >= size - 9) || (r >= size - 9 && c <= 8)) continue;
      for (let dr = -2; dr <= 2; dr++) {
        for (let dc = -2; dc <= 2; dc++) {
          const dark = Math.max(Math.abs(dr), Math.abs(dc)) !== 1;
          setFixed(r + dr, c + dc, dark);
        }
      }
    }
  }

  for (let i = 8; i < size - 8; i++) {
    if (!reserved[6][i]) setFixed(6, i, i % 2 === 0);
    if (!reserved[i][6]) setFixed(i, 6, i % 2 === 0);
  }

  setFixed(size - 8, 8, true);
  for (let i = 0; i < 9; i++) {
    if (!reserved[8][i]) reserved[8][i] = true;
    if (!reserved[i][8]) reserved[i][8] = true;
  }
  for (let i = 0; i < 8; i++) {
    reserved[8][size - 1 - i] = true;
    reserved[size - 1 - i][8] = true;
  }

  if (spec.version >= 7) {
    for (let i = 0; i < 18; i++) {
      const bit = ((spec.verInfo >> i) & 1) === 1;
      const a = Math.floor(i / 3);
      const b = size - 11 + (i % 3);
      setFixed(a, b, bit);
      setFixed(b, a, bit);
    }
  }

  const codewords = buildCodewords(bytes, spec);
  let bitIdx = 0;
  const totalBits = codewords.length * 8;
  let upward = true;

  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert++) {
      const r = upward ? size - 1 - vert : vert;
      for (let j = 0; j < 2; j++) {
        const c = right - j;
        if (reserved[r][c]) continue;
        let dark = false;
        if (bitIdx < totalBits) {
          const w = codewords[bitIdx >> 3];
          dark = ((w >> (7 - (bitIdx & 7))) & 1) === 1;
          bitIdx++;
        }
        if ((r + c) % 2 === 0) dark = !dark;
        modules[r][c] = dark;
      }
    }
    upward = !upward;
  }

  // Format information untuk Level L (01) + Mask 0 (000) = 0x77c4
  const fmt = 0x77c4;
  const c1Cols = [0, 1, 2, 3, 4, 5, 7, 8];
  for (let i = 0; i < 8; i++) {
    setFixed(8, c1Cols[i], ((fmt >> (14 - i)) & 1) === 1);
  }
  const c1Rows = [7, 5, 4, 3, 2, 1, 0];
  for (let i = 0; i < 7; i++) {
    setFixed(c1Rows[i], 8, ((fmt >> (6 - i)) & 1) === 1);
  }
  for (let i = 0; i < 7; i++) {
    setFixed(size - 1 - i, 8, ((fmt >> (14 - i)) & 1) === 1);
  }
  for (let i = 0; i < 8; i++) {
    setFixed(8, size - 8 + i, ((fmt >> (7 - i)) & 1) === 1);
  }

  return modules;
}

export function renderQRCanvas(text: string, cellSize = 4, margin = 4): HTMLCanvasElement {
  const matrix = encodeQRMatrix(text);
  const n = matrix.length;
  const px = (n + margin * 2) * cellSize;
  const canvas = document.createElement("canvas");
  canvas.width = px;
  canvas.height = px;
  canvas.setAttribute("data-testid", "mfa-qr-canvas");
  canvas.style.border = "1px solid #d9e2ec";
  canvas.style.borderRadius = "8px";
  canvas.style.background = "#ffffff";
  canvas.style.display = "block";
  canvas.style.margin = "8px 0";

  const ctx = canvas.getContext("2d");
  if (!ctx) return canvas;

  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, px, px);
  ctx.fillStyle = "#102a43";
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (matrix[r][c]) {
        ctx.fillRect((c + margin) * cellSize, (r + margin) * cellSize, cellSize, cellSize);
      }
    }
  }
  return canvas;
}
