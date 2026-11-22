package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// EnvVarSpec mendeskripsikan satu variabel konfigurasi environment dari struct tag AppConfig.
type EnvVarSpec struct {
	Name        string
	Type        string
	DefaultSafe string
	RequiredIn  string
	Secret      bool
	PortRole    string // "", "internal", "host"
	Component   string
	Description string
	Rationale   string
	Synthetic   string
}

// AppConfig merupakan satu-satunya sumber kebenaran (single source of truth)
// untuk 44 variabel konfigurasi environment aplikasi Kopdes Merah Putih.
type AppConfig struct {
	// --- Runtime, Network, & Worker (9) ---
	AppEnv         string   `env:"APP_ENV" type:"string" default:"development" required:"all" secret:"false" component:"api,db-init" desc:"Lingkungan runtime (development|test|staging|production)" rationale:"Membedakan guard ketat produksi (SSL, CORS non-wildcard, age) vs test/dev" example:"test"`
	AppTimezone    string   `env:"APP_TIMEZONE" type:"string" default:"Asia/Jakarta" required:"all" secret:"false" component:"api,Dockerfile" desc:"Zona waktu operasional (perhitungan batas harian & log)" rationale:"Menyelaraskan batas harian pilot, laporan, dan timestamp kontainer dengan zona koperasi" example:"Asia/Jakarta"`
	APIPort        int      `env:"API_PORT" type:"int" default:"8080" required:"all" secret:"false" port:"internal" component:"api,Dockerfile,compose" desc:"Port internal HTTP API di dalam proses/kontainer" rationale:"Mengikat listener API tanpa literal port agar portabel antar lingkungan" example:"8080"`
	APIHostPort    int      `env:"API_HOST_PORT" type:"int" default:"8080" required:"compose" secret:"false" port:"host" component:"compose,uitester" desc:"Port host publik yang dipetakan ke API_PORT" rationale:"Memetakan port host Docker Compose & target API uitester secara independen dari port kontainer" example:"8080"`
	UIPort         int      `env:"UI_PORT" type:"int" default:"8081" required:"all" secret:"false" port:"host" component:"uitester,compose" desc:"Port HTTP Frontend Tester/Wireframe lokal (dilarang aktif di production)" rationale:"Memisahkan port server UI uji agar Set A/Set B/Set C tidak bertabrakan" example:"8081"`
	WorkerPort     int      `env:"WORKER_PORT" type:"int" default:"8082" required:"optional" secret:"false" port:"host" component:"worker,compose,envcheck" desc:"Port loopback healthcheck worker (0 = nonaktif)" rationale:"Menyediakan /healthz proses kopdes worker terpantau Compose tanpa bentrok port" example:"8082"`
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" type:"[]string" default:"" required:"prod,staging" secret:"false" component:"api" desc:"Daftar origin CORS yang diizinkan (dipisah koma; dilarang * di production)" rationale:"Membatasi sumber browser sah untuk cookie HttpOnly + CSRFGuard di tiap VPS" example:"http://127.0.0.1:8081"`
	TrustedProxies []string `env:"TRUSTED_PROXIES" type:"[]string" default:"127.0.0.1/32" required:"prod" secret:"false" component:"api" desc:"Daftar CIDR reverse proxy tepercaya untuk X-Forwarded-For" rationale:"Mencegah spoofing IP limiter & memercayai X-Forwarded-Proto hanya dari Caddy/Cloudflare" example:"127.0.0.1/32"`
	RateLimitRPS   float64  `env:"RATE_LIMIT_RPS" type:"float64" default:"30" required:"optional" secret:"false" component:"api" desc:"Batas request per detik per IP (default aman 30 rps; hanya dilonggarkan pada APP_ENV=test)" rationale:"Mengontrol pembatas laju global & penskalaan uji ZAP penuh secara eksplisit dari .env" example:"30"`

	// --- Database PostgreSQL (8) ---
	DBHost           string `env:"DB_HOST" type:"string" default:"127.0.0.1" required:"all" secret:"false" component:"api,scripts" desc:"Hostname server PostgreSQL" rationale:"Menghubungkan biner lokal (127.0.0.1) maupun kontainer Compose (postgres)" example:"127.0.0.1"`
	DBPort           int    `env:"DB_PORT" type:"int" default:"5433" required:"all" secret:"false" port:"host" component:"api,scripts" desc:"Port koneksi PostgreSQL yang dituju API/skrip" rationale:"Portabilitas port DB non-standar antar stack uji & produksi tanpa ubah kode" example:"5433"`
	DBHostPort       int    `env:"DB_HOST_PORT" type:"int" default:"5433" required:"compose" secret:"false" port:"host" component:"compose" desc:"Port host PostgreSQL yang dipetakan Compose ke kontainer" rationale:"Mengisolasi port binding PostgreSQL pada 127.0.0.1 host Docker" example:"5433"`
	DBName           string `env:"DB_NAME" type:"string" default:"kopdes_db" required:"all" secret:"false" component:"api,compose,scripts" desc:"Nama database PostgreSQL (akhiran _test untuk DB uji)" rationale:"Memaksa guard seeder *_test serta memisahkan DB Set A/Set B/staging/produksi" example:"kopdes_test"`
	DBUser           string `env:"DB_USER" type:"string" default:"kopdes_app" required:"all" secret:"false" component:"api,db-init,scripts" desc:"Role PostgreSQL non-superuser aplikasi" rationale:"Menegakkan prinsip least-privilege role non-superuser pada runtime API/worker" example:"kopdes_app"`
	DBPassword       string `env:"DB_PASSWORD" type:"string" default:"" required:"all" secret:"true" component:"api,db-init" desc:"Kata sandi role DB_USER (kosong di .env.example)" rationale:"Kredensial rahasia DB per lingkungan yang dilarang masuk repositori" example:"kopdes_test_setA_pass_123!"`
	DBSSLMode        string `env:"DB_SSLMODE" type:"string" default:"disable" required:"all" secret:"false" component:"api,scripts" desc:"Mode SSL PostgreSQL (require/verify-full di production)" rationale:"Wajib require/verify-full pada profil produksi sekaligus mengizinkan disable pada CI/test lokal" example:"disable"`
	PostgresPassword string `env:"POSTGRES_PASSWORD" type:"string" default:"" required:"compose" secret:"true" component:"compose" desc:"Sandi bootstrap superuser/migrator Postgres kontainer (init only)" rationale:"Kredensial inisialisasi postgres:16-alpine yang terpisah dari role runtime kopdes_app" example:"pg_test_bootstrap_123!"`

	// --- Kriptografi & Sesi Autentikasi (8) ---
	AESMasterKey     string        `env:"AES_MASTER_KEY" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Kunci AES-256-GCM enkripsi PII/KYC/bukti (opsional prefix ver:id=hex)" rationale:"Kunci root HKDF-SHA256 enkripsi kolom PII, KYC, dan bukti pembayaran" example:"v1:k1=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	AESPreviousKeys  string        `env:"AES_PREVIOUS_KEYS" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Kunci AES historis format 'v1:k1=hex;v1:k2=hex' saat rotasi" rationale:"Memungkinkan dekripsi data lama selama rotasi kunci tanpa downtime" example:""`
	BlindIndexSecret string        `env:"BLIND_INDEX_SECRET" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Rahasia root HKDF-SHA256 untuk blind index NIK/email/token" rationale:"Kunci terpisah dari AES untuk pencocokan NIK/email/token anti-lookup-table" example:"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"`
	SessionIdleTTL   time.Duration `env:"SESSION_IDLE_TTL" type:"duration" default:"30m" required:"all" secret:"false" component:"api,frontend" desc:"Masa berlaku idle sesi opaque sebelum kedaluwarsa" rationale:"Batas idle kebijakan keamanan koperasi yang disinkronkan pula ke frontend via /public/config" example:"30m"`
	SessionAbsTTL    time.Duration `env:"SESSION_ABS_TTL" type:"duration" default:"12h" required:"all" secret:"false" component:"api,frontend" desc:"Masa berlaku absolut maksimum satu sesi login" rationale:"Menetapkan masa hidup sesi maksimal agar staf/anggota login ulang secara berkala" example:"12h"`
	CookieSecure     string        `env:"COOKIE_SECURE" type:"string" default:"auto" required:"all" secret:"false" component:"api,frontend" desc:"Kebijakan atribut Secure cookie sesi (auto|true|false; produksi wajib auto/true)" rationale:"Mengaktifkan __Host-kopdes_session pada HTTPS sambil mendukung dev HTTP lokal" example:"auto"`
	AllowBearerAuth  bool          `env:"ALLOW_BEARER_AUTH" type:"bool" default:"false" required:"optional" secret:"false" component:"api,frontend" desc:"Izinkan header Authorization Bearer untuk klien non-browser (default false di produksi)" rationale:"Menutup permukaan Bearer token di browser produksi sambil mengizinkan CLI/loadtest terkonfigurasi" example:"false"`
	MFARequiredRoles []string      `env:"MFA_REQUIRED_ROLES" type:"[]string" default:"admin,bendahara" required:"all" secret:"false" component:"api,frontend" desc:"Daftar peran yang wajib MFA TOTP" rationale:"Memungkinkan perluasan kewajiban TOTP (mis. menambah petugas_toko) tanpa rebuild biner" example:"admin,bendahara"`

	// --- CAPTCHA, Stok, & Pembayaran QRIS / Kendali Ganda (12) ---
	CaptchaEnabled               bool          `env:"CAPTCHA_ENABLED" type:"bool" default:"false" required:"optional" secret:"false" component:"api,frontend" desc:"Aktifkan verifikasi CAPTCHA Turnstile pada endpoint publik" rationale:"Saklar proteksi bot Turnstile antara lokal/CI vs produksi publik" example:"false"`
	CaptchaSiteKey               string        `env:"CAPTCHA_SITE_KEY" type:"string" default:"" required:"optional" secret:"false" component:"api,frontend" desc:"Public site key Turnstile untuk frontend" rationale:"Kunci publik Cloudflare Turnstile spesifik domain koperasi" example:""`
	CaptchaSecretKey             string        `env:"CAPTCHA_SECRET_KEY" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Secret key verifikasi server-side CAPTCHA Turnstile" rationale:"Rahasia verifikasi Turnstile di sisi server yang wajib di luar kode" example:""`
	PaymentsEnabled              bool          `env:"PAYMENTS_ENABLED" type:"bool" default:"true" required:"optional" secret:"false" component:"api,frontend" desc:"Status fitur pembuatan & verifikasi pembayaran" rationale:"Kill-switch cepat transaksi pembayaran jika ada insiden operasional/rekening" example:"true"`
	RegistrationEnabled          bool          `env:"REGISTRATION_ENABLED" type:"bool" default:"true" required:"optional" secret:"false" component:"api,frontend" desc:"Status fitur pendaftaran anggota baru & unggah KYC" rationale:"Kill-switch pendaftaran publik saat koperasi menutup rekrutmen sementara" example:"true"`
	AllowPersonalPaymentPilot    bool          `env:"ALLOW_PERSONAL_PAYMENT_PILOT" type:"bool" default:"false" required:"optional" secret:"false" component:"api,frontend" desc:"Izinkan rekening pilot perorangan terbatas batas nominal/harian" rationale:"Pembuka darurat rekening pilot sebelum QRIS badan hukum terbit (KEPUTUSAN PEMILIK)" example:"false"`
	PilotMaxPerTx                int64         `env:"PILOT_MAX_PER_TX" type:"int64" default:"500000" required:"optional" secret:"false" component:"api" desc:"Batas nominal per transaksi rekening pilot (Rupiah)" rationale:"Plafon per transaksi untuk mengendalikan risiko rekening pilot perorangan" example:"500000"`
	PilotMaxPerDay               int64         `env:"PILOT_MAX_PER_DAY" type:"int64" default:"2000000" required:"optional" secret:"false" component:"api" desc:"Batas nominal harian rekening pilot Asia/Jakarta (Rupiah)" rationale:"Plafon akumulasi harian rekening pilot Asia/Jakarta" example:"2000000"`
	QRISUniqueDigits             int           `env:"QRIS_UNIQUE_DIGITS" type:"int" default:"3" required:"optional" secret:"false" component:"api,frontend" desc:"Jumlah digit kode unik integer QRIS statis (3 atau 4)" rationale:"Menyesuaikan kapasitas antrean kode unik QRIS statis (3 vs 4 digit) milik koperasi" example:"3"`
	PaymentExpiryDuration        time.Duration `env:"PAYMENT_EXPIRY_DURATION" type:"duration" default:"15m" required:"optional" secret:"false" component:"api,frontend" desc:"Durasi kedaluwarsa tagihan pembayaran QRIS menunggu bayar" rationale:"Menentukan TTL pembayaran & countdown UI sebelum slot kode unik dilepas worker" example:"15m"`
	StockHoldMinutes             int           `env:"STOCK_HOLD_MINUTES" type:"int" default:"60" required:"optional" secret:"false" component:"api,cli" desc:"Durasi tahan (reservasi) stok pesanan dalam menit mencakup batas bayar + SLA verifikasi (KEPUTUSAN PEMILIK)" rationale:"Durasi reservasi stok sebelum dilepas worker (KEPUTUSAN PEMILIK sesuai jam layanan)" example:"60"`
	DualControlBreakglassEnabled bool          `env:"DUAL_CONTROL_BREAKGLASS_ENABLED" type:"bool" default:"false" required:"optional" secret:"false" component:"api" desc:"Izinkan mode darurat break-glass admin ber-MFA untuk kendali ganda (KEPUTUSAN PEMILIK)" rationale:"Kontrol darurat 1 admin bila bendahara berhalangan (KEPUTUSAN PEMILIK)" example:"false"`

	// --- Retensi PDP, Penyimpanan KYC/Bukti & Backup Terautentikasi (7) ---
	PDPRejectedRetentionDays int    `env:"PDP_REJECTED_RETENTION_DAYS" type:"int" default:"30" required:"optional" secret:"false" component:"api,cli" desc:"Retensi hari berkas pendaftaran ditolak sebelum purge (PERLU VERIFIKASI HUKUM)" rationale:"Batas simpan dokumen pendaftar ditolak untuk worker pdp_purge (PERLU VERIFIKASI HUKUM)" example:"30"`
	StorageKYCDir            string `env:"STORAGE_KYC_DIR" type:"string" default:"./storage/kyc" required:"all" secret:"false" component:"api,cli,compose" desc:"Direktori penyimpanan berkas KYC terenkripsi AES-256-GCM" rationale:"Memisahkan path/volume blob KYC terenkripsi antara Set A/Set B/kontainer" example:"./storage/kyc"`
	StorageBuktiDir          string `env:"STORAGE_BUKTI_DIR" type:"string" default:"./storage/bukti" required:"all" secret:"false" component:"api,cli,compose" desc:"Direktori/volume penyimpanan berkas bukti bayar & refund terenkripsi terpisah dari KYC" rationale:"Memisahkan path/volume bukti bayar dari KYC sesuai isolasi ruang simpan" example:"./storage/bukti"`
	BuktiRetentionDays       int    `env:"BUKTI_RETENTION_DAYS" type:"int" default:"365" required:"optional" secret:"false" component:"api,cli" desc:"Retensi hari berkas bukti bayar non-lunas sebelum purge (PERLU VERIFIKASI HUKUM)" rationale:"Masa retensi bukti non-lunas sebelum dibersihkan worker bukti_purge (PERLU VERIFIKASI HUKUM)" example:"365"`
	BackupDir                string `env:"BACKUP_DIR" type:"string" default:"./backups" required:"scripts" secret:"false" component:"scripts" desc:"Direktori penyimpanan arsip backup PostgreSQL & berkas terenkripsi" rationale:"Menentukan direktori target backup.sh / restore_drill.sh agar terisolasi per lingkungan" example:"./backups"`
	BackupAgeRecipient       string `env:"BACKUP_AGE_RECIPIENT" type:"string" default:"" required:"prod" secret:"false" component:"scripts" desc:"Public key penerima age (age1...) untuk enkripsi backup terautentikasi; private key WAJIB disimpan di luar server" rationale:"Menghindari penempatan private key di VPS dengan enkripsi asimetris age" example:"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"`
	BackupRetentionDays      int    `env:"BACKUP_RETENTION_DAYS" type:"int" default:"30" required:"scripts" secret:"false" component:"scripts" desc:"Jumlah hari retensi arsip backup lokal" rationale:"Mengatur jumlah hari arsip .tar.age sebelum rotasi otomatis oleh backup.sh" example:"30"`

	// DatabaseURL opsional dari OS env/PaaS (tidak masuk daftar 44 var .env) untuk deteksi konflik DSN.
	DatabaseURL string
}

// DefaultDBAppRole adalah role PostgreSQL non-superuser wajib untuk koneksi runtime API.
const DefaultDBAppRole = "kopdes_app"

// Load membaca seluruh parameter AppConfig dari fungsi lookup (mis. os.LookupEnv).
func Load(lookup func(string) (string, bool)) (AppConfig, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	var cfg AppConfig
	v := reflect.ValueOf(&cfg).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		envKey := field.Tag.Get("env")
		if envKey == "" {
			continue
		}
		raw, ok := lookup(envKey)
		if !ok || strings.TrimSpace(raw) == "" {
			raw = field.Tag.Get("default")
		} else {
			raw = strings.TrimSpace(raw)
		}
		if err := assignField(v.Field(i), envKey, raw); err != nil {
			return cfg, err
		}
	}

	if rawURL, ok := lookup("DATABASE_URL"); ok && strings.TrimSpace(rawURL) != "" {
		cfg.DatabaseURL = strings.TrimSpace(rawURL)
	}

	if err := cfg.validateBasic(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func assignField(fv reflect.Value, envKey, raw string) error {
	if fv.Kind() == reflect.String {
		fv.SetString(raw)
		return nil
	}
	if raw == "" {
		return nil
	}
	switch fv.Kind() {
	case reflect.Int:
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("konfigurasi %s tidak valid (%q): harus bilangan bulat", envKey, raw)
		}
		fv.SetInt(int64(parsed))
	case reflect.Int64:
		if fv.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("konfigurasi %s tidak valid (%q): harus format durasi Go (mis. 15m)", envKey, raw)
			}
			fv.SetInt(int64(d))
			return nil
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("konfigurasi %s tidak valid (%q): harus int64", envKey, raw)
		}
		fv.SetInt(parsed)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("konfigurasi %s tidak valid (%q): harus true/false", envKey, raw)
		}
		fv.SetBool(parsed)
	case reflect.Float64:
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("konfigurasi %s tidak valid (%q): harus bilangan desimal", envKey, raw)
		}
		fv.SetFloat(parsed)
	case reflect.Slice:
		if fv.Type().Elem().Kind() == reflect.String {
			var items []string
			for _, part := range strings.Split(raw, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					items = append(items, trimmed)
				}
			}
			fv.Set(reflect.ValueOf(items))
		}
	}
	return nil
}

// LoadFromEnv memuat konfigurasi menggunakan os.LookupEnv.
func LoadFromEnv() (AppConfig, error) {
	return Load(os.LookupEnv)
}

func (c AppConfig) validateBasic() error {
	for _, pair := range []struct {
		name string
		val  int
	}{
		{"API_PORT", c.APIPort},
		{"API_HOST_PORT", c.APIHostPort},
		{"UI_PORT", c.UIPort},
		{"DB_PORT", c.DBPort},
		{"DB_HOST_PORT", c.DBHostPort},
	} {
		if pair.val <= 0 || pair.val > 65535 {
			return fmt.Errorf("%s di luar jangkauan: %d", pair.name, pair.val)
		}
	}
	if c.WorkerPort < 0 || c.WorkerPort > 65535 {
		return fmt.Errorf("WORKER_PORT di luar jangkauan: %d", c.WorkerPort)
	}
	if err := c.validateSessionAndLimits(); err != nil {
		return err
	}
	if c.AppTimezone != "" {
		if _, err := time.LoadLocation(c.AppTimezone); err != nil {
			return fmt.Errorf("APP_TIMEZONE tidak dikenal %q: %w", c.AppTimezone, err)
		}
	}
	if strings.TrimSpace(c.DatabaseURL) != "" {
		return c.validateDatabaseURLConsistency()
	}
	return nil
}

func (c AppConfig) validateSessionAndLimits() error {
	if c.SessionIdleTTL <= 0 || c.SessionAbsTTL < c.SessionIdleTTL {
		return fmt.Errorf("SESSION_IDLE_TTL (%s) harus > 0 dan <= SESSION_ABS_TTL (%s)", c.SessionIdleTTL, c.SessionAbsTTL)
	}
	if len(c.MFARequiredRoles) == 0 {
		return fmt.Errorf("MFA_REQUIRED_ROLES minimal berisi satu peran")
	}
	for _, r := range c.MFARequiredRoles {
		if !slices.Contains([]string{"admin", "bendahara", "petugas_toko", "anggota"}, strings.ToLower(strings.TrimSpace(r))) {
			return fmt.Errorf("MFA_REQUIRED_ROLES memuat peran tidak valid %q", r)
		}
	}
	if c.PilotMaxPerTx <= 0 || c.PilotMaxPerDay < c.PilotMaxPerTx {
		return fmt.Errorf("PILOT_MAX_PER_TX dan PILOT_MAX_PER_DAY tidak valid")
	}
	if c.QRISUniqueDigits != 3 && c.QRISUniqueDigits != 4 {
		return fmt.Errorf("QRIS_UNIQUE_DIGITS harus 3 atau 4, dapat %d", c.QRISUniqueDigits)
	}
	if c.PaymentExpiryDuration <= 0 {
		return fmt.Errorf("PAYMENT_EXPIRY_DURATION wajib > 0")
	}
	if c.PDPRejectedRetentionDays <= 0 || c.BackupRetentionDays <= 0 || c.BuktiRetentionDays <= 0 {
		return fmt.Errorf("retensi PDP, bukti, dan backup wajib > 0 hari")
	}
	if c.StockHoldMinutes <= 0 {
		return fmt.Errorf("STOCK_HOLD_MINUTES wajib > 0")
	}
	cs := strings.ToLower(strings.TrimSpace(c.CookieSecure))
	if cs != "auto" && cs != "true" && cs != "false" {
		return fmt.Errorf("COOKIE_SECURE harus auto, true, atau false (dapat %q)", c.CookieSecure)
	}
	if rec := strings.TrimSpace(c.BackupAgeRecipient); rec != "" && !strings.HasPrefix(rec, "age1") {
		return fmt.Errorf("BACKUP_AGE_RECIPIENT harus berupa public key age berawalan age1")
	}
	if c.RateLimitRPS <= 0 {
		return fmt.Errorf("RATE_LIMIT_RPS wajib > 0 (dapat %v)", c.RateLimitRPS)
	}
	return nil
}

// ValidateProduction memvalidasi kesiapan konfigurasi ketat untuk lingkungan produksi.
func (c AppConfig) ValidateProduction() error {
	if c.RateLimitRPS < 1 || c.RateLimitRPS > 120 {
		return fmt.Errorf("RATE_LIMIT_RPS produksi wajib dalam rentang 1..120 (dapat %v)", c.RateLimitRPS)
	}
	if strings.EqualFold(c.DBSSLMode, "disable") {
		return fmt.Errorf("DB_SSLMODE tidak boleh disable di produksi")
	}
	if strings.TrimSpace(c.DBPassword) == "" {
		return fmt.Errorf("DB_PASSWORD wajib diisi di produksi")
	}
	if strings.EqualFold(strings.TrimSpace(c.CookieSecure), "false") {
		return fmt.Errorf("COOKIE_SECURE tidak boleh false di produksi")
	}
	if c.CaptchaEnabled && (strings.TrimSpace(c.CaptchaSiteKey) == "" || strings.TrimSpace(c.CaptchaSecretKey) == "") {
		return fmt.Errorf("CAPTCHA_SITE_KEY dan CAPTCHA_SECRET_KEY wajib diisi bila CAPTCHA_ENABLED=true di produksi")
	}
	if rec := strings.TrimSpace(c.BackupAgeRecipient); rec == "" || !strings.HasPrefix(rec, "age1") {
		return fmt.Errorf("BACKUP_AGE_RECIPIENT wajib diisi public key age1... di produksi")
	}
	for _, o := range c.AllowedOrigins {
		if strings.TrimSpace(o) == "*" {
			return fmt.Errorf("ALLOWED_ORIGINS tidak boleh berisi wildcard '*'")
		}
	}
	for _, cidr := range c.TrustedProxies {
		trimmed := strings.TrimSpace(cidr)
		if trimmed == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(trimmed)
		if err != nil {
			return fmt.Errorf("TRUSTED_PROXIES berisi CIDR tidak valid %q: %w", cidr, err)
		}
		ones, _ := ipNet.Mask.Size()
		if ones < 16 {
			return fmt.Errorf("TRUSTED_PROXIES %q terlalu lebar untuk mode production (maksimal /16, ditemukan /%d)", cidr, ones)
		}
	}
	return nil
}

// BuildDatabaseDSN merakit DSN PostgreSQL secara deterministik dari DB_HOST, DB_PORT,
// DB_NAME, DB_USER, DB_PASSWORD, dan DB_SSLMODE. Jika DATABASE_URL ikut diset,
// sistem memverifikasi kesesuaian penuh tanpa pernah mencetak password.
func (c AppConfig) BuildDatabaseDSN() (string, error) {
	if strings.TrimSpace(c.DBHost) == "" {
		return "", fmt.Errorf("DB_HOST wajib diisi")
	}
	if c.DBPort <= 0 || c.DBPort > 65535 {
		return "", fmt.Errorf("DB_PORT tidak valid: %d", c.DBPort)
	}
	if strings.TrimSpace(c.DBName) == "" {
		return "", fmt.Errorf("DB_NAME wajib diisi")
	}
	if strings.TrimSpace(c.DBUser) == "" {
		return "", fmt.Errorf("DB_USER wajib diisi")
	}
	if strings.TrimSpace(c.DBPassword) == "" {
		return "", fmt.Errorf("DB_PASSWORD wajib diisi")
	}
	sslMode := strings.TrimSpace(c.DBSSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}
	if strings.TrimSpace(c.DatabaseURL) != "" {
		if err := c.validateDatabaseURLConsistency(); err != nil {
			return "", err
		}
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   fmt.Sprintf("%s:%d", c.DBHost, c.DBPort),
		Path:   "/" + c.DBName,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c AppConfig) validateDatabaseURLConsistency() error {
	parsed, err := url.Parse(strings.TrimSpace(c.DatabaseURL))
	if err != nil {
		return fmt.Errorf("DATABASE_URL tidak valid secara sintaks (detail URL disembunyikan demi keamanan sandi)")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("DATABASE_URL harus berskema postgres/postgresql, dapat %q", parsed.Scheme)
	}
	urlHost := parsed.Hostname()
	urlPortStr := parsed.Port()
	urlDB := strings.TrimPrefix(parsed.Path, "/")
	urlUser := ""
	urlPass, hasURLPass := "", false
	if parsed.User != nil {
		urlUser = parsed.User.Username()
		urlPass, hasURLPass = parsed.User.Password()
	}

	if c.DBHost != "" && !strings.EqualFold(urlHost, c.DBHost) {
		return fmt.Errorf("konflik konfigurasi DB_HOST=%q vs DATABASE_URL host=%q", c.DBHost, urlHost)
	}
	if urlPortStr != "" && strconv.Itoa(c.DBPort) != urlPortStr {
		return fmt.Errorf("konflik konfigurasi DB_PORT=%d vs DATABASE_URL port=%s", c.DBPort, urlPortStr)
	}
	if c.DBName != "" && urlDB != c.DBName {
		return fmt.Errorf("konflik konfigurasi DB_NAME=%q vs DATABASE_URL database=%q", c.DBName, urlDB)
	}
	if c.DBUser != "" && urlUser != c.DBUser {
		return fmt.Errorf("konflik konfigurasi DB_USER=%q vs DATABASE_URL user=%q", c.DBUser, urlUser)
	}
	if hasURLPass && c.DBPassword != "" && urlPass != c.DBPassword {
		return fmt.Errorf("konflik konfigurasi DB_PASSWORD vs sandi di DATABASE_URL (nilai sandi disembunyikan)")
	}
	if qSSL := parsed.Query().Get("sslmode"); qSSL != "" && c.DBSSLMode != "" && !strings.EqualFold(qSSL, c.DBSSLMode) {
		return fmt.Errorf("konflik konfigurasi DB_SSLMODE=%q vs DATABASE_URL sslmode=%q", c.DBSSLMode, qSSL)
	}
	return nil
}

// Catalog mengembalikan daftar spesifikasi 44 variabel environment secara otomatis dari tag struct AppConfig.
func Catalog() []EnvVarSpec {
	t := reflect.TypeOf(AppConfig{})
	specs := make([]EnvVarSpec, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("env")
		if name == "" {
			continue
		}
		specs = append(specs, EnvVarSpec{
			Name:        name,
			Type:        f.Tag.Get("type"),
			DefaultSafe: f.Tag.Get("default"),
			RequiredIn:  f.Tag.Get("required"),
			Secret:      f.Tag.Get("secret") == "true",
			PortRole:    f.Tag.Get("port"),
			Component:   f.Tag.Get("component"),
			Description: f.Tag.Get("desc"),
			Rationale:   f.Tag.Get("rationale"),
			Synthetic:   f.Tag.Get("example"),
		})
	}
	return specs
}

func renderRemovedEnvVarsSection(b *strings.Builder) {
	b.WriteString("## 3. Variabel yang Dihapus untuk Ramping (51 -> 44 Variabel <= 45)\n\n")
	b.WriteString("| Variabel Dihapus | Nilai Bawaan Aman / Pengganti | Alasan Penghapusan |\n")
	b.WriteString("|---|---|---|\n")
	b.WriteString("| `SLO_READ_P95_MS` | Default `300` ms (`tools/loadtest`, `tests/perf/k6_mixed.js`) | Tidak dibaca biner API/worker produksi; cukup diatur perkakas uji |\n")
	b.WriteString("| `SLO_WRITE_P95_MS` | Default `800` ms (`tools/loadtest`, `tests/perf/k6_mixed.js`) | Tidak dibaca biner API/worker produksi; cukup diatur perkakas uji |\n")
	b.WriteString("| `SLO_MAX_ERROR_RATE` | Default `0.005` (`tools/loadtest`, `tests/perf/k6_mixed.js`) | Tidak dibaca biner API/worker produksi; cukup diatur perkakas uji |\n")
	b.WriteString("| `WORKER_TICK_INTERVAL` | Default aman `60s` (`kopdes worker --tick`) | Sudah tersedia flag CLI `--tick`; tidak perlu menuhankan `.env` |\n")
	b.WriteString("| `LOGIN_MAX_ATTEMPTS` | Default OWASP `5` di `api.DefaultConfig()` | Batas brute-force standar cukup dikunci aman pada kode |\n")
	b.WriteString("| `LOGIN_LOCKOUT_DURATION` | Default OWASP `15m` di `api.DefaultConfig()` | Durasi lockout standar cukup dikunci aman pada kode |\n")
	b.WriteString("| `PENDING_LIMIT_PER_MEMBER` | Default aman `2` di `api.DefaultConfig()` | Kebijakan internal aman koperasi tunggal tanpa perlu knob opsional |\n")
	b.WriteString("| `PENDING_LIMIT_GLOBAL` | Default aman `50` di `api.DefaultConfig()` | Kapasitas aman ruang kode unik (3/4 digit) di `api.DefaultConfig()` |\n\n")
}

// RenderEnvReferenceMarkdown menghasilkan isi lengkap docs/ENV_REFERENCE.md langsung dari AppConfig.
func RenderEnvReferenceMarkdown() string {
	specs := Catalog()
	var b strings.Builder
	b.WriteString("# Referensi Variabel Environment — Kopdes Merah Putih\n\n")
	b.WriteString("> **Dokumen ini di-generate otomatis** dari struct `AppConfig` di `internal/config/config.go`.\n")
	b.WriteString("> Total variabel: **" + strconv.Itoa(len(specs)) + "** (target ramping <= 45 variabel).\n\n")

	b.WriteString("## 1. Pemisahan Konfigurasi: Environment vs Database (`pengaturan`)\n\n")
	b.WriteString("| Jenis Pengaturan | Lokasi Penyimpanan | Contoh Parameter |\n")
	b.WriteString("|---|---|---|\n")
	b.WriteString("| Infrastruktur, Port, DB, Kunci AES/HMAC, Batas Sesi & Keamanan | Variabel Environment (`.env`) | `API_PORT`, `DB_*`, `AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, `SESSION_*`, `MFA_REQUIRED_ROLES` |\n")
	b.WriteString("| Profil & Kebijakan Operasional Koperasi Tunggal | Tabel DB `pengaturan` (1 baris, `id = 1`) | `nama_koperasi`, `alamat_koperasi`, `ongkir_tetap`, `qris_unique_digits`, `active_rekening_qris_id` |\n\n")

	b.WriteString("## 2. Pemisahan Port Internal vs Host\n\n")
	b.WriteString("| Peran Port | Variabel | Fungsi |\n")
	b.WriteString("|---|---|---|\n")
	for _, s := range specs {
		if s.PortRole != "" {
			b.WriteString(fmt.Sprintf("| `%s` | `%s` | %s |\n", s.PortRole, s.Name, s.Description))
		}
	}
	b.WriteString("\n")
	renderRemovedEnvVarsSection(&b)

	b.WriteString("## 4. Katalog Lengkap 44 Variabel Environment & Alasan Tertulis\n\n")
	b.WriteString("| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | ALASAN DIPERTAHANKAN | CONTOH NILAI SINTETIS |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range specs {
		defVal := "`" + s.DefaultSafe + "`"
		if s.DefaultSafe == "" {
			defVal = "_(kosong)_"
		}
		rahasia := "Tidak"
		if s.Secret {
			rahasia = "**Ya**"
		}
		synth := "_(opsional)_"
		if s.Synthetic != "" {
			synth = "`" + s.Synthetic + "`"
		}
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | `%s` | %s | `%s` | %s | %s | %s |\n",
			s.Name, s.Type, defVal, s.RequiredIn, rahasia, s.Component, s.Description, s.Rationale, synth))
	}
	return b.String()
}
