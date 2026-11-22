package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecisionAudit_D3_CookieOnlyAndCSRFAndProxySecure(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")

	// 1. Server produksi-like behind trusted proxy: CookieSecure=auto, TrustedProxies=["127.0.0.1/32"], AllowBearerAuth=false
	cfgTrusted := ConfigFromAppConfig(e.appCfg)
	cfgTrusted.StorageKYCDir = e.kycDir
	cfgTrusted.StorageBuktiDir = e.buktiDir
	cfgTrusted.CookieSecure = "auto"
	cfgTrusted.TrustedProxies = []string{"127.0.0.1/32"}
	cfgTrusted.AllowBearerAuth = false
	cfgTrusted.AllowedOrigins = []string{"https://app.kopdes.example"}

	trustedSrv := httptest.NewServer(NewServer(e.pool, cfgTrusted).Router())
	defer trustedSrv.Close()

	// CORS preflight & Vary: Origin
	optReq, _ := http.NewRequest(http.MethodOptions, trustedSrv.URL+"/api/v1/auth/login", nil)
	optReq.Header.Set("Origin", "https://app.kopdes.example")
	optResp, err := http.DefaultClient.Do(optReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = optResp.Body.Close()
	if !strings.Contains(strings.Join(optResp.Header.Values("Vary"), ","), "Origin") {
		t.Fatalf("expected Vary: Origin header, got %v", optResp.Header)
	}

	// Login dengan X-Forwarded-Proto: https dari trusted proxy -> cookie __Host-kopdes_session Secure, session_token TIDAK dikirim di JSON
	loginBody, _ := json.Marshal(map[string]string{"identifier": "anggota1@kopdes-test.local", "password": e.seedPwd})
	lReq, _ := http.NewRequest(http.MethodPost, trustedSrv.URL+"/api/v1/auth/login", bytes.NewReader(loginBody))
	lReq.Header.Set("Content-Type", "application/json")
	lReq.Header.Set("X-Forwarded-Proto", "https")
	lResp, err := http.DefaultClient.Do(lReq)
	if err != nil {
		t.Fatal(err)
	}
	lRaw, _ := io.ReadAll(lResp.Body)
	_ = lResp.Body.Close()
	if lResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d %s", lResp.StatusCode, string(lRaw))
	}
	if strings.Contains(string(lRaw), "session_token") {
		t.Fatalf("ALLOW_BEARER_AUTH=false must omit session_token from JSON body, got: %s", string(lRaw))
	}
	var hostCookie *http.Cookie
	for _, c := range lResp.Cookies() {
		if c.Name == "__Host-kopdes_session" {
			hostCookie = c
		}
	}
	if hostCookie == nil || !hostCookie.Secure || !hostCookie.HttpOnly || hostCookie.Path != "/" || hostCookie.Domain != "" || hostCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected valid __Host-kopdes_session cookie behind trusted HTTPS proxy, got %+v (all=%+v)", hostCookie, lResp.Cookies())
	}

	// Bearer token auth harus ditolak 401 ketika ALLOW_BEARER_AUTH=false
	bReq := mustNewReq(t, http.MethodGet, trustedSrv.URL+"/api/v1/auth/me", hostCookie.Value)
	bResp, err := http.DefaultClient.Do(bReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = bResp.Body.Close()
	if bResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when Bearer sent while ALLOW_BEARER_AUTH=false, got %d", bResp.StatusCode)
	}

	// 2. Untrusted proxy spoofing X-Forwarded-Proto: https -> diabaikan, cookie non-Secure kopdes_session
	cfgUntrusted := cfgTrusted
	cfgUntrusted.TrustedProxies = []string{"10.0.0.1/32"}
	untrustedSrv := httptest.NewServer(NewServer(e.pool, cfgUntrusted).Router())
	defer untrustedSrv.Close()
	uReq, _ := http.NewRequest(http.MethodPost, untrustedSrv.URL+"/api/v1/auth/login", bytes.NewReader(loginBody))
	uReq.Header.Set("Content-Type", "application/json")
	uReq.Header.Set("X-Forwarded-Proto", "https")
	uResp, err := http.DefaultClient.Do(uReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = uResp.Body.Close()
	for _, c := range uResp.Cookies() {
		if c.Secure || c.Name == "__Host-kopdes_session" {
			t.Fatalf("untrusted X-Forwarded-Proto spoof must NOT produce Secure cookie, got %+v", c)
		}
	}

	// 3. Simulasi browser full round-trip + CSRF + reload + logout menggunakan TLS server (__Host- cookie jar)
	tlsSrv := httptest.NewTLSServer(NewServer(e.pool, cfgTrusted).Router())
	defer tlsSrv.Close()
	jar, _ := cookiejar.New(nil)
	browser := tlsSrv.Client()
	browser.Jar = jar

	tlReq, _ := http.NewRequest(http.MethodPost, tlsSrv.URL+"/api/v1/auth/login", bytes.NewReader(loginBody))
	tlReq.Header.Set("Content-Type", "application/json")
	tlReq.Header.Set("Origin", "https://app.kopdes.example")
	tlReq.Header.Set("X-Requested-With", "kopdes")
	tlResp, err := browser.Do(tlReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = tlResp.Body.Close()
	if tlResp.StatusCode != http.StatusOK {
		t.Fatalf("TLS browser login failed: %d", tlResp.StatusCode)
	}

	// Simulasi reload halaman: GET /api/v1/auth/me hanya bermodal cookie HttpOnly -> 200
	meResp, err := browser.Get(tlsSrv.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	_ = meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after page reload with HttpOnly cookie, got %d", meResp.StatusCode)
	}

	// CSRF test 1: Origin jahat dengan cookie aktif -> 403
	evilReq, _ := http.NewRequest(http.MethodPost, tlsSrv.URL+"/api/v1/auth/logout", strings.NewReader(`{}`))
	evilReq.Header.Set("Origin", "https://evil.example")
	evilReq.Header.Set("X-Requested-With", "kopdes")
	evilResp, err := browser.Do(evilReq)
	if err != nil {
		t.Fatal(err)
	}
	evilRaw, _ := io.ReadAll(evilResp.Body)
	_ = evilResp.Body.Close()
	if evilResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-origin cookie POST, got %d %s", evilResp.StatusCode, string(evilRaw))
	}

	// CSRF test 2: Origin diizinkan tetapi TANPA X-Requested-With -> 403
	noHdrReq, _ := http.NewRequest(http.MethodPost, tlsSrv.URL+"/api/v1/auth/logout", strings.NewReader(`{}`))
	noHdrReq.Header.Set("Origin", "https://app.kopdes.example")
	noHdrResp, err := browser.Do(noHdrReq)
	if err != nil {
		t.Fatal(err)
	}
	noHdrRaw, _ := io.ReadAll(noHdrResp.Body)
	_ = noHdrResp.Body.Close()
	if noHdrResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when X-Requested-With missing on cookie POST, got %d %s", noHdrResp.StatusCode, string(noHdrRaw))
	}

	// Logout sah -> 200 & hapus cookie & cabut sesi di DB
	outReq, _ := http.NewRequest(http.MethodPost, tlsSrv.URL+"/api/v1/auth/logout", strings.NewReader(`{}`))
	outReq.Header.Set("Origin", "https://app.kopdes.example")
	outReq.Header.Set("X-Requested-With", "kopdes")
	outResp, err := browser.Do(outReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = outResp.Body.Close()
	if outResp.StatusCode != http.StatusOK {
		t.Fatalf("valid logout failed: %d", outResp.StatusCode)
	}
	afterOut, err := browser.Get(tlsSrv.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	_ = afterOut.Body.Close()
	if afterOut.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout cleared cookie, got %d", afterOut.StatusCode)
	}
}

func TestDecisionAudit_D9_SessionTouchThrottleAndInstantRevocation(t *testing.T) {
	e := setupE2EEnv(t, ".env.test")
	tok := loginSession(t, e, "anggota1@kopdes-test.local", e.seedPwd, "")

	var initLastActive time.Time
	err := e.pool.QueryRow(context.Background(), `
		SELECT last_active_at
		FROM sesi
		ORDER BY created_at DESC LIMIT 1`).Scan(&initLastActive)
	if err != nil {
		t.Fatalf("query initial session row: %v", err)
	}

	// 20 request cepat dalam <60 detik -> last_active_at TIDAK boleh di-UPDATE berulang kali
	time.Sleep(15 * time.Millisecond)
	start := time.Now()
	for i := 0; i < 20; i++ {
		st, _, raw := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/auth/me", tok, nil, nil)
		if st != http.StatusOK {
			t.Fatalf("request %d failed: %d %s", i, st, string(raw))
		}
	}
	elapsed := time.Since(start)

	var afterBurstLastActive time.Time
	if err := e.pool.QueryRow(context.Background(), `
		SELECT last_active_at FROM sesi ORDER BY created_at DESC LIMIT 1`).Scan(&afterBurstLastActive); err != nil {
		t.Fatal(err)
	}
	if !afterBurstLastActive.Equal(initLastActive) {
		t.Fatalf("D9 throttle gagal: last_active_at berubah dalam <60s (%v -> %v)", initLastActive, afterBurstLastActive)
	}
	t.Logf("D9 measurement: 20 rapid GET /auth/me finished in %v with 0 DB session writes", elapsed)

	// Jika last_active_at > 60 detik lalu -> permintaan berikutnya WAJIB memperbarui last_active_at
	if _, err := e.pool.Exec(context.Background(), `UPDATE sesi SET last_active_at = NOW() - INTERVAL '90 seconds'`); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/auth/me", tok, nil, nil); st != http.StatusOK {
		t.Fatalf("expected 200 when refreshing stale session activity, got %d", st)
	}
	var refreshedLastActive time.Time
	if err := e.pool.QueryRow(context.Background(), `
		SELECT last_active_at FROM sesi ORDER BY created_at DESC LIMIT 1`).Scan(&refreshedLastActive); err != nil {
		t.Fatal(err)
	}
	if time.Since(refreshedLastActive) > 10*time.Second {
		t.Fatalf("expected last_active_at updated to ~NOW(), got %v", refreshedLastActive)
	}

	// Pencabutan seketika saat sesi di-revoke meskipun masih dalam jendela throttle 60s
	if _, err := e.pool.Exec(context.Background(), `UPDATE sesi SET revoked_at = NOW()`); err != nil {
		t.Fatal(err)
	}
	if revSt, _, _ := doJSONReq(t, http.MethodGet, e.httpSrv.URL+"/api/v1/auth/me", tok, nil, nil); revSt != http.StatusUnauthorized {
		t.Fatalf("revoked session must immediately return 401 within throttle window, got %d", revSt)
	}
}
