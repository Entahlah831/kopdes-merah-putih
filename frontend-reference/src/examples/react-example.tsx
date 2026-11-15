import { useState, useEffect, useCallback, type FormEvent } from "react";
import {
  createClient,
  fetchPublicConfig,
  createPaymentPoller,
  formatRupiah,
  getErrorPresentation,
  compressImageInBrowser,
  validateUploadFile,
  ApiError,
  type PublicConfig,
  type AuthSessionUser,
} from "../index.js";

export type AuthUiStep = "anonymous" | "mfa_setup_required" | "mfa_challenge" | "authenticated";

export function useKopdesAuth(baseUrl: string) {
  const [config, setConfig] = useState<PublicConfig | null>(null);
  const [client, setClient] = useState<ReturnType<typeof createClient> | null>(null);
  const [step, setStep] = useState<AuthUiStep>("anonymous");
  const [user, setUser] = useState<AuthSessionUser | null>(null);
  const [errorBanner, setErrorBanner] = useState<string>("");

  useEffect(() => {
    const ac = new AbortController();
    fetchPublicConfig(baseUrl, { signal: ac.signal })
      .then((cfg) => {
        setConfig(cfg);
        setClient(
          createClient({
            baseUrl,
            publicConfig: cfg,
            onForceLogout: () => {
              setUser(null);
              setStep("anonymous");
            },
            onConfigChange: setConfig,
          })
        );
      })
      .catch((err) => {
        setErrorBanner(err instanceof Error ? err.message : "Gagal memuat konfigurasi");
      });
    return () => ac.abort();
  }, [baseUrl]);

  const login = useCallback(
    async (email: string, password: string) => {
      if (!client) return;
      setErrorBanner("");
      try {
        const res = await client.auth.login(email, password);
        if (res.status === "mfa_setup_required") {
          setStep("mfa_setup_required");
        } else if (res.status === "mfa_challenge") {
          setStep("mfa_challenge");
        } else {
          setUser(res.session);
          setStep("authenticated");
        }
      } catch (err) {
        if (err instanceof ApiError) {
          const ui = getErrorPresentation(err);
          setErrorBanner(`${ui.title}: ${ui.message}`);
        }
      }
    },
    [client]
  );

  const verifyMfa = useCallback(
    async (totpCode: string) => {
      if (!client) return;
      setErrorBanner("");
      const session = await client.auth.verifyMFA({ totpCode });
      setUser(session);
      setStep("authenticated");
    },
    [client]
  );

  const logout = useCallback(async () => {
    if (!client) return;
    await client.auth.logout();
    setUser(null);
    setStep("anonymous");
  }, [client]);

  return { config, client, step, user, errorBanner, login, verifyMfa, logout };
}

export function QrisPaymentCard({
  client,
  config,
  tagihanIds,
}: {
  client: ReturnType<typeof createClient>;
  config: PublicConfig;
  tagihanIds: string[];
}) {
  const [idempKey, setIdempKey] = useState(() => client.newIdempotencyKey());
  const [busy, setBusy] = useState(false);
  const [qrData, setQrData] = useState<{
    pembayaran_id: string;
    nominal_total: number;
    kode_unik: number;
    qris_payload: string;
    is_autolock_nominal: boolean;
    warning_banner?: string;
    berlaku_sampai: string;
  } | null>(null);
  const [liveStatus, setLiveStatus] = useState("");
  const [errMsg, setErrMsg] = useState("");

  useEffect(() => {
    setIdempKey(client.newIdempotencyKey());
  }, [client, tagihanIds.join(",")]);

  useEffect(() => {
    if (!qrData) return;
    const poller = createPaymentPoller({
      intervalMs: config.polling_interval_ms,
      expiresAtMs: Date.parse(qrData.berlaku_sampai),
      fetchStatus: (signal) => client.request<{ status: string }>(`/api/v1/pembayaran/${qrData.pembayaran_id}`, { signal }),
      onUpdate: (st) => setLiveStatus(st.status),
    });
    poller.start();
    return () => poller.stop();
  }, [qrData, client, config.polling_interval_ms]);

  const startPay = async () => {
    if (!config.features.payments_enabled || config.features.read_only_mode) return;
    setBusy(true);
    setErrMsg("");
    try {
      const data = await client.createQRISPayment<typeof qrData>(
        { jenis_transaksi: "iuran_bulanan", tagihan_iuran_id: tagihanIds },
        idempKey
      );
      setQrData(data);
    } catch (e) {
      if (e instanceof ApiError) setErrMsg(getErrorPresentation(e).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section aria-label="Pembayaran QRIS">
      <button type="button" disabled={busy || !config.features.payments_enabled} onClick={startPay}>
        {busy ? "Memproses..." : "Buat Kode Bayar QRIS"}
      </button>
      {qrData && (
        <div>
          <p>Total Transfer Tepat: {formatRupiah(qrData.nominal_total)}</p>
          <p>Kode Unik: {qrData.kode_unik}</p>
          {qrData.warning_banner && <p role="alert">{qrData.warning_banner}</p>}
          <p role="status">Status: {liveStatus || "menunggu_pembayaran"}</p>
        </div>
      )}
      {errMsg && <p role="alert">{errMsg}</p>}
    </section>
  );
}

export function PendaftaranKycCard({
  client,
  config,
  wilayahDesaId,
}: {
  client: ReturnType<typeof createClient>;
  config: PublicConfig;
  wilayahDesaId: string;
}) {
  const [statusMsg, setStatusMsg] = useState("");
  const [progress, setProgress] = useState(0);

  const onSubmit = async (ev: FormEvent<HTMLFormElement>) => {
    ev.preventDefault();
    const form = new FormData(ev.currentTarget);
    const created = await client.request<{ pendaftaran_id: string }>("/api/v1/pendaftaran", {
      method: "POST",
      body: {
        nik: form.get("nik"),
        nama_lengkap: form.get("nama_lengkap"),
        tempat_lahir: form.get("tempat_lahir"),
        tanggal_lahir: form.get("tanggal_lahir"),
        jenis_kelamin: form.get("jenis_kelamin"),
        alamat_ktp: form.get("alamat_ktp"),
        wilayah_desa_id: wilayahDesaId,
      },
    });
    const rawFile = form.get("ktp") as File | null;
    if (rawFile) {
      const check = validateUploadFile(rawFile, {
        maxBytes: config.upload_limits.max_kyc_upload_bytes * 2,
        allowedMimeTypes: config.upload_limits.allowed_mime_types,
      });
      if (!check.ok) {
        setStatusMsg(check.reason ?? "Berkas tidak valid");
        return;
      }
      const compressed = await compressImageInBrowser(rawFile, {
        maxWidth: 1600,
        quality: 0.8,
        maxBytes: config.upload_limits.max_kyc_upload_bytes,
      });
      const up = new FormData();
      up.append("dokumen", compressed);
      up.append("jenis_dokumen", "ktp");
      await client.uploadMultipart(`/api/v1/pendaftaran/${created.pendaftaran_id}/dokumen`, up, {
        onProgress: setProgress,
      });
    }
    setStatusMsg("Pendaftaran terkirim.");
  };

  return (
    <form onSubmit={onSubmit}>
      <input name="nik" required minLength={16} maxLength={16} />
      <input name="nama_lengkap" required />
      <input name="tempat_lahir" required />
      <input name="tanggal_lahir" type="date" required />
      <input name="jenis_kelamin" defaultValue="L" />
      <input name="alamat_ktp" required />
      <input name="ktp" type="file" accept={config.upload_limits.allowed_mime_types.join(",")} required />
      <button type="submit">Kirim Pendaftaran ({progress}%)</button>
      {statusMsg && <p role="status">{statusMsg}</p>}
    </form>
  );
}
