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
	Synthetic   string
}

// AppConfig merupakan satu-satunya sumber kebenaran (single source of truth)
// untuk 40 variabel konfigurasi environment aplikasi Kopdes Merah Putih.
type AppConfig struct {
	// --- Runtime & Network (7) ---
	AppEnv         string   `env:"APP_ENV" type:"string" default:"development" required:"all" secret:"false" component:"api,db-init" desc:"Lingkungan runtime (development|test|staging|production)" example:"test"`
	AppTimezone    string   `env:"APP_TIMEZONE" type:"string" default:"Asia/Jakarta" required:"all" secret:"false" component:"api,Dockerfile" desc:"Zona waktu operasional (perhitungan batas harian & log)" example:"Asia/Jakarta"`
	APIPort        int      `env:"API_PORT" type:"int" default:"8080" required:"all" secret:"false" port:"internal" component:"api,Dockerfile,compose" desc:"Port internal HTTP API di dalam proses/kontainer" example:"8080"`
	APIHostPort    int      `env:"API_HOST_PORT" type:"int" default:"8080" required:"compose" secret:"false" port:"host" component:"compose,uitester" desc:"Port host publik yang dipetakan ke API_PORT" example:"8080"`
	UIPort         int      `env:"UI_PORT" type:"int" default:"8081" required:"all" secret:"false" port:"host" component:"uitester,compose" desc:"Port HTTP Frontend Tester lokal/uji (dilarang aktif di production)" example:"8081"`
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" type:"[]string" default:"" required:"prod,staging" secret:"false" component:"api" desc:"Daftar origin CORS yang diizinkan (dipisah koma; dilarang * di production)" example:"http://127.0.0.1:8081"`
	TrustedProxies []string `env:"TRUSTED_PROXIES" type:"[]string" default:"127.0.0.1/32" required:"prod" secret:"false" component:"api" desc:"Daftar CIDR reverse proxy tepercaya untuk X-Forwarded-For" example:"127.0.0.1/32"`

	// --- Database PostgreSQL (8) ---
	DBHost           string `env:"DB_HOST" type:"string" default:"127.0.0.1" required:"all" secret:"false" component:"api,scripts" desc:"Hostname server PostgreSQL" example:"127.0.0.1"`
	DBPort           int    `env:"DB_PORT" type:"int" default:"5433" required:"all" secret:"false" port:"host" component:"api,scripts" desc:"Port koneksi PostgreSQL yang dituju API/skrip" example:"5433"`
	DBHostPort       int    `env:"DB_HOST_PORT" type:"int" default:"5433" required:"compose" secret:"false" port:"host" component:"compose" desc:"Port host PostgreSQL yang dipetakan Compose ke kontainer" example:"5433"`
	DBName           string `env:"DB_NAME" type:"string" default:"kopdes_db" required:"all" secret:"false" component:"api,compose,scripts" desc:"Nama database PostgreSQL (akhiran _test untuk DB uji)" example:"kopdes_test"`
	DBUser           string `env:"DB_USER" type:"string" default:"kopdes_app" required:"all" secret:"false" component:"api,db-init,scripts" desc:"Role PostgreSQL non-superuser aplikasi" example:"kopdes_app"`
	DBPassword       string `env:"DB_PASSWORD" type:"string" default:"" required:"all" secret:"true" component:"api,db-init" desc:"Kata sandi role DB_USER (kosong di .env.example)" example:"kopdes_test_setA_pass_123!"`
	DBSSLMode        string `env:"DB_SSLMODE" type:"string" default:"disable" required:"all" secret:"false" component:"api,scripts" desc:"Mode SSL PostgreSQL (require/verify-full di production)" example:"disable"`
	PostgresPassword string `env:"POSTGRES_PASSWORD" type:"string" default:"" required:"compose" secret:"true" component:"compose" desc:"Sandi bootstrap superuser postgres di kontainer (init only)" example:"pg_test_bootstrap_123!"`

	// --- Kriptografi & Sesi Autentikasi (8) ---
	AESMasterKey         string        `env:"AES_MASTER_KEY" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Kunci AES-256-GCM enkripsi PII/KYC (opsional prefix ver:id=hex)" example:"v1:k1=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	AESPreviousKeys      string        `env:"AES_PREVIOUS_KEYS" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Kunci AES historis format 'v1:k1=hex;v1:k2=hex' saat rotasi" example:""`
	BlindIndexSecret     string        `env:"BLIND_INDEX_SECRET" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Rahasia root HKDF-SHA256 untuk blind index NIK/email/token" example:"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"`
	SessionIdleTTL       time.Duration `env:"SESSION_IDLE_TTL" type:"duration" default:"30m" required:"all" secret:"false" component:"api,frontend" desc:"Masa berlaku idle sesi opaque sebelum kedaluwarsa" example:"30m"`
	SessionAbsTTL        time.Duration `env:"SESSION_ABS_TTL" type:"duration" default:"12h" required:"all" secret:"false" component:"api,frontend" desc:"Masa berlaku absolut maksimum satu sesi login" example:"12h"`
	MFARequiredRoles     []string      `env:"MFA_REQUIRED_ROLES" type:"[]string" default:"admin,bendahara" required:"all" secret:"false" component:"api,frontend" desc:"Daftar peran yang wajib MFA TOTP" example:"admin,bendahara"`
	LoginMaxAttempts     int           `env:"LOGIN_MAX_ATTEMPTS" type:"int" default:"5" required:"all" secret:"false" component:"api" desc:"Batas kegagalan login sebelum kunci sementara" example:"5"`
	LoginLockoutDuration time.Duration `env:"LOGIN_LOCKOUT_DURATION" type:"duration" default:"15m" required:"all" secret:"false" component:"api" desc:"Durasi penguncian login setelah batas percobaan terlampaui" example:"15m"`

	// --- CAPTCHA & Pembayaran QRIS / Pilot (12) ---
	CaptchaEnabled            bool          `env:"CAPTCHA_ENABLED" type:"bool" default:"false" required:"optional" secret:"false" component:"api,frontend" desc:"Aktifkan verifikasi CAPTCHA Turnstile pada endpoint publik" example:"false"`
	CaptchaSiteKey            string        `env:"CAPTCHA_SITE_KEY" type:"string" default:"" required:"optional" secret:"false" component:"api,frontend" desc:"Public site key Turnstile untuk frontend" example:""`
	CaptchaSecretKey          string        `env:"CAPTCHA_SECRET_KEY" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Secret key verifikasi server-side CAPTCHA Turnstile" example:""`
	PaymentsEnabled           bool          `env:"PAYMENTS_ENABLED" type:"bool" default:"true" required:"optional" secret:"false" component:"api,frontend" desc:"Status fitur pembuatan & verifikasi pembayaran" example:"true"`
	RegistrationEnabled       bool          `env:"REGISTRATION_ENABLED" type:"bool" default:"true" required:"optional" secret:"false" component:"api,frontend" desc:"Status fitur pendaftaran anggota baru & unggah KYC" example:"true"`
	AllowPersonalPaymentPilot bool          `env:"ALLOW_PERSONAL_PAYMENT_PILOT" type:"bool" default:"false" required:"optional" secret:"false" component:"api,frontend" desc:"Izinkan rekening pilot perorangan terbatas batas nominal/harian" example:"false"`
	PilotMaxPerTx             int64         `env:"PILOT_MAX_PER_TX" type:"int64" default:"500000" required:"optional" secret:"false" component:"api" desc:"Batas nominal per transaksi rekening pilot (Rupiah)" example:"500000"`
	PilotMaxPerDay            int64         `env:"PILOT_MAX_PER_DAY" type:"int64" default:"2000000" required:"optional" secret:"false" component:"api" desc:"Batas nominal harian rekening pilot Asia/Jakarta (Rupiah)" example:"2000000"`
	QRISUniqueDigits          int           `env:"QRIS_UNIQUE_DIGITS" type:"int" default:"3" required:"optional" secret:"false" component:"api,frontend" desc:"Jumlah digit kode unik integer QRIS statis (3 atau 4)" example:"3"`
	PaymentExpiryDuration     time.Duration `env:"PAYMENT_EXPIRY_DURATION" type:"duration" default:"15m" required:"optional" secret:"false" component:"api,frontend" desc:"Durasi kedaluwarsa tagihan pembayaran QRIS menunggu bayar" example:"15m"`
	PendingLimitPerMember     int           `env:"PENDING_LIMIT_PER_MEMBER" type:"int" default:"2" required:"optional" secret:"false" component:"api" desc:"Batas maksimal pembayaran QRIS berstatus menunggu per anggota" example:"2"`
	PendingLimitGlobal        int           `env:"PENDING_LIMIT_GLOBAL" type:"int" default:"50" required:"optional" secret:"false" component:"api" desc:"Batas global pembayaran QRIS aktif bersamaan untuk ruang kode unik" example:"50"`

	// --- Retensi PDP, Penyimpanan KYC & Backup (5) ---
	PDPRejectedRetentionDays int    `env:"PDP_REJECTED_RETENTION_DAYS" type:"int" default:"30" required:"optional" secret:"false" component:"api,cli" desc:"Retensi hari berkas pendaftaran ditolak sebelum purge (PERLU VERIFIKASI HUKUM)" example:"30"`
	StorageKYCDir            string `env:"STORAGE_KYC_DIR" type:"string" default:"./storage/kyc" required:"all" secret:"false" component:"api,compose" desc:"Direktori penyimpanan berkas KYC terenkripsi AES-256-GCM" example:"./storage/kyc"`
	BackupDir                string `env:"BACKUP_DIR" type:"string" default:"./backups" required:"scripts" secret:"false" component:"scripts" desc:"Direktori penyimpanan arsip backup PostgreSQL terenkripsi" example:"./backups"`
	BackupEncryptionKey      string `env:"BACKUP_ENCRYPTION_KEY" type:"string" default:"" required:"prod" secret:"true" component:"scripts" desc:"Passphrase OpenSSL AES-256-CBC untuk enkripsi arsip backup" example:"setA_backup_enc_key_0123456789abcdef!"`
	BackupRetentionDays      int    `env:"BACKUP_RETENTION_DAYS" type:"int" default:"30" required:"scripts" secret:"false" component:"scripts" desc:"Jumlah hari retensi arsip backup lokal" example:"30"`

	// DatabaseURL opsional dari OS env/PaaS (tidak masuk daftar 40 var .env) untuk deteksi konflik DSN.
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
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Int:
		if raw == "" {
			return nil
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("konfigurasi %s tidak valid (%q): harus bilangan bulat", envKey, raw)
		}
		fv.SetInt(int64(parsed))
	case reflect.Int64:
		if raw == "" {
			return nil
		}
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
		if raw == "" {
			return nil
		}
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("konfigurasi %s tidak valid (%q): harus true/false", envKey, raw)
		}
		fv.SetBool(parsed)
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
	if c.LoginMaxAttempts <= 0 || c.LoginLockoutDuration <= 0 {
		return fmt.Errorf("LOGIN_MAX_ATTEMPTS dan LOGIN_LOCKOUT_DURATION wajib > 0")
	}
	if c.PilotMaxPerTx <= 0 || c.PilotMaxPerDay < c.PilotMaxPerTx {
		return fmt.Errorf("PILOT_MAX_PER_TX dan PILOT_MAX_PER_DAY tidak valid")
	}
	if c.QRISUniqueDigits != 3 && c.QRISUniqueDigits != 4 {
		return fmt.Errorf("QRIS_UNIQUE_DIGITS harus 3 atau 4, dapat %d", c.QRISUniqueDigits)
	}
	if c.PaymentExpiryDuration <= 0 || c.PendingLimitPerMember <= 0 || c.PendingLimitGlobal < c.PendingLimitPerMember {
		return fmt.Errorf("batas pembayaran/pending QRIS tidak valid")
	}
	if c.PDPRejectedRetentionDays <= 0 || c.BackupRetentionDays <= 0 {
		return fmt.Errorf("retensi PDP dan backup wajib > 0 hari")
	}
	return nil
}

// ValidateProduction memvalidasi kesiapan konfigurasi ketat untuk lingkungan produksi.
func (c AppConfig) ValidateProduction() error {
	if strings.EqualFold(c.DBSSLMode, "disable") {
		return fmt.Errorf("DB_SSLMODE tidak boleh disable di produksi")
	}
	if strings.TrimSpace(c.DBPassword) == "" {
		return fmt.Errorf("DB_PASSWORD wajib diisi di produksi")
	}
	if c.CaptchaEnabled && (strings.TrimSpace(c.CaptchaSiteKey) == "" || strings.TrimSpace(c.CaptchaSecretKey) == "") {
		return fmt.Errorf("CAPTCHA_SITE_KEY dan CAPTCHA_SECRET_KEY wajib diisi bila CAPTCHA_ENABLED=true di produksi")
	}
	if strings.TrimSpace(c.BackupEncryptionKey) == "" {
		return fmt.Errorf("BACKUP_ENCRYPTION_KEY wajib diisi di produksi")
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

// Catalog mengembalikan daftar spesifikasi 40 variabel environment secara otomatis dari tag struct AppConfig.
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
			Synthetic:   f.Tag.Get("example"),
		})
	}
	return specs
}

// RenderEnvReferenceMarkdown menghasilkan isi lengkap docs/ENV_REFERENCE.md langsung dari AppConfig.
func RenderEnvReferenceMarkdown() string {
	specs := Catalog()
	var b strings.Builder
	b.WriteString("# Referensi Variabel Environment — Kopdes Merah Putih\n\n")
	b.WriteString("> **Dokumen ini di-generate otomatis** dari struct `AppConfig` di `internal/config/config.go`.\n")
	b.WriteString("> Total variabel: **" + strconv.Itoa(len(specs)) + "** (maksimal 40 variabel).\n\n")

	b.WriteString("## 1. Pemisahan Konfigurasi: Environment vs Database (`pengaturan`)\n\n")
	b.WriteString("| Jenis Pengaturan | Lokasi Penyimpanan | Contoh Parameter |\n")
	b.WriteString("|---|---|---|\n")
	b.WriteString("| Infrastruktur, Port, DB, Kunci AES/HMAC, Batas Sesi & Keamanan | Variabel Environment (`.env`) | `API_PORT`, `DB_*`, `AES_MASTER_KEY`, `BLIND_INDEX_SECRET`, `SESSION_*`, `MFA_REQUIRED_ROLES` |\n")
	b.WriteString("| Profil & Kebijakan Operasional Koperasi Tunggal | Tabel DB `pengaturan` (1 baris, `id = 1`) | `nama_koperasi`, `alamat_koperasi`, `ongkir_tetap`, `qris_unique_digits`, `active_rekening_qris_id` |\n\n")

	b.WriteString("## 2. Pemisahan Port Internal vs Host\n\n")
	b.WriteString("| Peran Port | Variabel | Fungsi |\n")
	b.WriteString("|---|---|---|\n")
	for _, s := range specs {
		if s.PortRole == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | %s |\n", s.PortRole, s.Name, s.Description))
	}
	b.WriteString("\n")

	b.WriteString("## 3. Katalog Lengkap Variabel Environment\n\n")
	b.WriteString("| NAMA | TIPE | DEFAULT AMAN | WAJIB DI | RAHASIA? | DIBACA OLEH | PENJELASAN | CONTOH NILAI SINTETIS |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, s := range specs {
		defVal := s.DefaultSafe
		if defVal == "" {
			defVal = "_(kosong)_"
		} else {
			defVal = "`" + defVal + "`"
		}
		rahasia := "Tidak"
		if s.Secret {
			rahasia = "**Ya**"
		}
		synth := s.Synthetic
		if synth == "" {
			synth = "_(opsional)_"
		} else {
			synth = "`" + synth + "`"
		}
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | `%s` | %s | `%s` | %s | %s |\n",
			s.Name, s.Type, defVal, s.RequiredIn, rahasia, s.Component, s.Description, synth))
	}
	return b.String()
}
