import { APIClient } from "../api";
import { clear, el } from "../dom";
import { validateAndCompressImage } from "../image";
import { renderQRCanvas } from "../qr";
import { RouteKey, UserSession } from "../types";

export function renderDaftar(container: HTMLElement, api: APIClient, navigate: (r: RouteKey) => void): void {
  const pub = api.getPublic();
  const feedback = el("div");
  const kycSection = el("div", { class: "card", style: "display:none;" });

  if (pub && pub.features && !pub.features.registration_enabled) {
    container.appendChild(el("div", { class: "alert alert-error" }, "Pendaftaran anggota baru sedang dinonaktifkan oleh koperasi."));
    return;
  }

  const form = el("form", { class: "card", "data-testid": "form-daftar" });
  const inNama = el("input", { type: "text", name: "nama_lengkap", required: true, placeholder: "Nama sesuai KTP" });
  const inEmail = el("input", { type: "email", name: "email", required: true, placeholder: "warga@contoh.id" });
  const inNIK = el("input", { type: "text", name: "nik", required: true, placeholder: "16 digit NIK" });
  const inHP = el("input", { type: "text", name: "no_hp", required: true, placeholder: "0812xxxxxxxx" });
  const inTgl = el("input", { type: "date", name: "tanggal_lahir" });
  const inPekerjaan = el("input", { type: "text", name: "pekerjaan", required: true, placeholder: "Petani / Pedagang / dll." });
  const inAlamat = el("textarea", { name: "alamat", required: true, rows: "2", placeholder: "Alamat domisili di desa/kelurahan koperasi" });
  const chkDom = el("input", { type: "checkbox", id: "chk-domisili", style: "width:auto;" });
  const chkPDP = el("input", { type: "checkbox", id: "chk-pdp", style: "width:auto;" });
  const btnSubmit = el("button", { class: "btn btn-primary", type: "submit", "data-testid": "btn-kirim-daftar" }, "Kirim Pendaftaran");

  form.append(
    el("h2", null, "Daftar Calon Anggota"),
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Nama Lengkap *"), inNama),
      el("div", { class: "field" }, el("label", null, "Email *"), inEmail),
      el("div", { class: "field" }, el("label", null, "NIK (16 digit) *"), inNIK),
      el("div", { class: "field" }, el("label", null, "Nomor HP *"), inHP),
      el("div", { class: "field" }, el("label", null, "Tanggal Lahir"), inTgl),
      el("div", { class: "field" }, el("label", null, "Pekerjaan *"), inPekerjaan)
    ),
    el("div", { class: "field" }, el("label", null, "Alamat Domisili *"), inAlamat),
    el("div", { class: "field" }, el("label", { class: "row" }, chkDom, el("span", null, "Saya berdomisili satu wilayah dengan koperasi desa."))),
    el("div", { class: "field" }, el("label", { class: "row" }, chkPDP, el("span", null, "Saya menyetujui pemrosesan data pribadi sesuai UU No. 27/2022 (UU PDP). [PERLU VERIFIKASI HUKUM]"))),
    btnSubmit
  );

  // Panel Unggah KYC 3 Dokumen wajib
  let currentRegID = "";
  let currentUploadToken = "";
  const kycFeedback = el("div");
  const kycJenis = el("select", { "data-testid": "kyc-tipe" },
    el("option", { value: "ktp" }, "KTP (ktp)"),
    el("option", { value: "selfie_ktp" }, "Selfie dengan KTP (selfie_ktp)"),
    el("option", { value: "kk" }, "Kartu Keluarga (kk)")
  );
  const kycFile = el("input", { type: "file", accept: "image/jpeg,image/png", "data-testid": "kyc-file" });
  const btnUploadKYC = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-upload-kyc" }, "Validasi Canvas & Unggah Dokumen");
  const kycList = el("ul");

  kycSection.append(
    el("h3", null, "Langkah 2 — Unggah Berkas KYC Terenkripsi"),
    el("p", { class: "alert alert-note" }, "Hanya JPG/PNG (PDF & dokumen Surat Pernyataan ditolak; diproses aman melalui kanvas browser sebelum dienkripsi server)."),
    kycFeedback,
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Tipe Dokumen"), kycJenis),
      el("div", { class: "field" }, el("label", null, "Pilih Foto JPG/PNG"), kycFile)
    ),
    btnUploadKYC,
    kycList
  );

  btnUploadKYC.addEventListener("click", async () => {
    kycFeedback.textContent = "";
    const file = kycFile.files?.[0];
    if (!file) {
      kycFeedback.appendChild(el("div", { class: "alert alert-error" }, "Pilih berkas JPG/PNG terlebih dahulu."));
      return;
    }
    btnUploadKYC.disabled = true;
    try {
      const processed = await validateAndCompressImage(file);
      const fd = new FormData();
      fd.append("tipe_dokumen", kycJenis.value);
      fd.append("file", processed.blob, processed.filename);
      await api.request("POST", `/api/v1/public/pendaftaran/${encodeURIComponent(currentRegID)}/dokumen`, {
        formData: fd,
        headers: { "X-Upload-Token": currentUploadToken }
      });
      kycFeedback.appendChild(el("div", { class: "alert alert-ok" }, `Dokumen ${kycJenis.value} berhasil terenkripsi (${processed.width}x${processed.height}).`));
      kycList.appendChild(el("li", null, `Terkirim terenkripsi: ${kycJenis.value} (${processed.filename})`));
    } catch (err) {
      kycFeedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    } finally {
      btnUploadKYC.disabled = false;
    }
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    feedback.textContent = "";
    if (!chkDom.checked || !chkPDP.checked) {
      feedback.appendChild(el("div", { class: "alert alert-error" }, "Harap centang pernyataan domisili dan persetujuan UU PDP."));
      return;
    }
    btnSubmit.disabled = true;
    try {
      const payload: Record<string, string> = {
        nama_lengkap: inNama.value.trim(),
        email: inEmail.value.trim(),
        nik: inNIK.value.trim(),
        no_hp: inHP.value.trim(),
        pekerjaan: inPekerjaan.value.trim(),
        alamat: inAlamat.value.trim()
      };
      if (inTgl.value) payload.tanggal_lahir = inTgl.value;
      const res = await api.request<{ id: string; upload_token: string }>("POST", "/api/v1/public/pendaftaran", { body: payload });
      currentRegID = res.data.id;
      currentUploadToken = res.data.upload_token;
      feedback.appendChild(el("div", { class: "alert alert-ok", "data-testid": "daftar-sukses" }, `Pendaftaran diterima! ID: ${currentRegID}. Lanjutkan unggah foto KTP.`));
      kycSection.style.display = "block";
    } catch (err) {
      feedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    } finally {
      btnSubmit.disabled = false;
    }
  });

  // Panel Aktivasi Sandi dengan Activation Code
  const actCard = el("form", { class: "card", "data-testid": "form-aktivasi" });
  const actCode = el("input", { type: "text", placeholder: "Kode aktivasi dari pengurus", "data-testid": "act-code" });
  const actPass = el("input", { type: "password", placeholder: "Minimal 12 karakter kuat", "data-testid": "act-pass" });
  const actFeedback = el("div");
  actCard.append(
    el("h3", null, "Sudah Disetujui? Aktivasi Kata Sandi Anggota"),
    actFeedback,
    el("div", { class: "grid-cards" },
      el("div", { class: "field" }, el("label", null, "Kode Aktivasi"), actCode),
      el("div", { class: "field" }, el("label", null, "Kata Sandi Baru"), actPass)
    ),
    el("div", { class: "row" },
      el("button", { class: "btn btn-primary", type: "submit", "data-testid": "btn-aktivasi" }, "Aktifkan Akun"),
      el("button", { class: "btn", type: "button", id: "go-login-after-act" }, "Ke Halaman Masuk ->")
    )
  );
  actCard.querySelector("#go-login-after-act")?.addEventListener("click", () => navigate("masuk"));
  actCard.addEventListener("submit", async (e) => {
    e.preventDefault();
    actFeedback.textContent = "";
    try {
      await api.request("POST", "/api/v1/auth/password", {
        body: { code: actCode.value.trim(), new_password: actPass.value }
      });
      actFeedback.appendChild(el("div", { class: "alert alert-ok", "data-testid": "act-sukses" }, "Kata sandi aktif! Silakan login melalui halaman Masuk."));
    } catch (err) {
      actFeedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    }
  });

  container.append(feedback, form, kycSection, actCard);
}

export function renderMasuk(
  container: HTMLElement,
  api: APIClient,
  onLoggedIn: (user: UserSession) => void
): void {
  const feedback = el("div");
  const mfaBox = el("div", { class: "card", style: "display:none;" });

  const form = el("form", { class: "card", "data-testid": "form-login" });
  const inIdent = el("input", { type: "text", required: true, placeholder: "Email atau No HP", "data-testid": "login-identifier" });
  const inPass = el("input", { type: "password", required: true, placeholder: "Kata sandi", "data-testid": "login-password" });
  const btnLogin = el("button", { class: "btn btn-primary", type: "submit", "data-testid": "btn-login" }, "Masuk");

  form.append(
    el("h2", null, "Masuk Portal Koperasi"),
    feedback,
    el("div", { class: "field" }, el("label", null, "Email / No. HP"), inIdent),
    el("div", { class: "field" }, el("label", null, "Kata Sandi"), inPass),
    btnLogin
  );

  let mfaSetupToken = "";
  let mfaMode: "setup" | "verify" = "verify";
  const mfaCode = el("input", { type: "text", placeholder: "6 digit kode TOTP", "data-testid": "mfa-code" });
  const btnMFA = el("button", { class: "btn btn-primary", type: "button", "data-testid": "btn-mfa-submit" }, "Verifikasi MFA TOTP");
  const btnSkipMFA = el(
    "button",
    { class: "btn", type: "button", style: "display:none;", "data-testid": "btn-mfa-skip" },
    "Lanjutkan Nanti (Atur di Pengaturan Akun)"
  );
  const mfaInfo = el("div", { class: "alert alert-note" });
  const qrWrap = el("div", { style: "margin:8px 0;" });
  mfaBox.append(
    el("h3", null, "Verifikasi 2-Langkah (MFA TOTP)"),
    mfaInfo,
    qrWrap,
    el("div", { class: "field" }, el("label", null, "Kode OTP 6 Digit"), mfaCode),
    el("div", { class: "row" }, btnMFA, btnSkipMFA)
  );

  btnMFA.addEventListener("click", async () => {
    feedback.textContent = "";
    btnMFA.disabled = true;
    try {
      if (mfaMode === "setup") {
        await api.request("POST", "/api/v1/auth/mfa/setup", {
          body: { setup_token: mfaSetupToken, code: mfaCode.value.trim() }
        });
      } else {
        await api.request("POST", "/api/v1/auth/mfa/verify", { body: { code: mfaCode.value.trim() } });
      }
      const me = await api.request<UserSession>("GET", "/api/v1/auth/me");
      onLoggedIn(me.data);
    } catch (err) {
      feedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    } finally {
      btnMFA.disabled = false;
    }
  });

  btnSkipMFA.addEventListener("click", async () => {
    feedback.textContent = "";
    btnSkipMFA.disabled = true;
    try {
      await api.request("POST", "/api/v1/auth/mfa/setup", { body: { action: "skip" } });
      const me = await api.request<UserSession>("GET", "/api/v1/auth/me");
      onLoggedIn(me.data);
    } catch (err) {
      feedback.appendChild(el("div", { class: "alert alert-error" }, (err as Error).message));
    } finally {
      btnSkipMFA.disabled = false;
    }
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    feedback.textContent = "";
    btnLogin.disabled = true;
    try {
      const res = await api.request<{ scope?: string; requires_mfa?: boolean; mfa_setup_required?: boolean }>("POST", "/api/v1/auth/login", {
        body: { identifier: inIdent.value.trim(), password: inPass.value }
      });
      const scope = res.data?.scope || "";
      if (scope === "mfa_setup" || res.data?.mfa_setup_required) {
        mfaMode = "setup";
        const setupRes = await api.request<{ secret: string; otpauth_uri?: string; setup_token: string }>("POST", "/api/v1/auth/mfa/setup", { body: {} });
        mfaSetupToken = setupRes.data.setup_token;
        mfaInfo.textContent = `MFA belum aktif. Pindai QR Code di bawah dengan Google Authenticator / Authy (atau ketik manual Secret TOTP: ${setupRes.data.secret}) — masukkan kode 6 digit untuk aktivasi, atau klik Lanjutkan Nanti.`;
        clear(qrWrap);
        const qrUri = setupRes.data.otpauth_uri || `otpauth://totp/Kopdes:${encodeURIComponent(inIdent.value.trim())}?secret=${setupRes.data.secret}&issuer=Kopdes`;
        qrWrap.appendChild(renderQRCanvas(qrUri, 4));
        btnSkipMFA.style.display = "inline-block";
        mfaBox.style.display = "block";
        return;
      }
      if (scope === "mfa_pending" || res.data?.requires_mfa) {
        mfaMode = "verify";
        mfaInfo.textContent = "Masukkan 6 digit kode TOTP aktif akun Anda.";
        clear(qrWrap);
        btnSkipMFA.style.display = "none";
        mfaBox.style.display = "block";
        return;
      }
      const me = await api.request<UserSession>("GET", "/api/v1/auth/me");
      onLoggedIn(me.data);
    } catch (err) {
      feedback.appendChild(el("div", { class: "alert alert-error", "data-testid": "login-error" }, (err as Error).message));
    } finally {
      btnLogin.disabled = false;
    }
  });

  container.append(form, mfaBox);
}
