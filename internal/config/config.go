package config

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
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
	PortRole    string // "", "internal", "host", "bind"
	Component   string
	Description string
	Synthetic   string
}

// AppConfig merupakan satu-satunya sumber kebenaran (single source of truth)
// untuk seluruh konfigurasi environment aplikasi Kopdes Merah Putih.
type AppConfig struct {
	// --- Runtime & Network ---
	AppEnv         string   `env:"APP_ENV" type:"string" default:"development" required:"all" secret:"false" component:"api,db-init" desc:"Lingkungan runtime (development|test|staging|production)" example:"test"`
	AppTimezone    string   `env:"APP_TIMEZONE" type:"string" default:"Asia/Jakarta" required:"all" secret:"false" component:"api,Dockerfile" desc:"Zona waktu operasional (perhitungan batas harian & log)" example:"Asia/Jakarta"`
	APIPort        int      `env:"API_PORT" type:"int" default:"8080" required:"all" secret:"false" port:"internal" component:"api,Dockerfile,compose" desc:"Port internal HTTP API di dalam kontainer/proses" example:"8080"`
	APIHostPort    int      `env:"API_HOST_PORT" type:"int" default:"8080" required:"compose" secret:"false" port:"host" component:"compose" desc:"Port host publik yang dipetakan ke API_PORT" example:"8080"`
	MetricsPort    int      `env:"METRICS_PORT" type:"int" default:"9100" required:"prod,compose" secret:"false" port:"internal" component:"api,compose" desc:"Port internal endpoint Prometheus /metrics (bila berbeda dari API_PORT)" example:"9100"`
	MetricsHostPort int     `env:"METRICS_HOST_PORT" type:"int" default:"9100" required:"compose" secret:"false" port:"host" component:"compose,prometheus" desc:"Port host untuk eksposur /metrics internal" example:"9100"`
	PrometheusHostPort int  `env:"PROMETHEUS_HOST_PORT" type:"int" default:"9090" required:"monitoring" secret:"false" port:"host" component:"prometheus" desc:"Port host layanan Prometheus opsional" example:"9090"`
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" type:"[]string" default:"" required:"prod,staging" secret:"false" component:"api" desc:"Daftar origin CORS yang diizinkan (dipisah koma; dilarang * di production)" example:"http://127.0.0.1:3000"`
	TrustedProxies []string `env:"TRUSTED_PROXIES" type:"[]string" default:"127.0.0.1/32" required:"prod" secret:"false" component:"api" desc:"Daftar CIDR reverse proxy tepercaya untuk X-Forwarded-For" example:"127.0.0.1/32"`

	// --- Database PostgreSQL ---
	DBHost             string        `env:"DB_HOST" type:"string" default:"127.0.0.1" required:"all" secret:"false" component:"api,scripts" desc:"Hostname server PostgreSQL 16" example:"127.0.0.1"`
	DBPort             int           `env:"DB_PORT" type:"int" default:"5433" required:"all" secret:"false" port:"host" component:"api,scripts" desc:"Port koneksi DB yang dituju klien/API di host atau jaringan" example:"5433"`
	DBContainerPort    int           `env:"DB_CONTAINER_PORT" type:"int" default:"5432" required:"compose" secret:"false" port:"internal" component:"compose" desc:"Port internal PostgreSQL di dalam kontainer" example:"5432"`
	DBHostPort         int           `env:"DB_HOST_PORT" type:"int" default:"5433" required:"compose" secret:"false" port:"host" component:"compose" desc:"Port host PostgreSQL yang dipetakan ke DB_CONTAINER_PORT" example:"5433"`
	DBBindHost         string        `env:"DB_BIND_HOST" type:"string" default:"127.0.0.1" required:"compose" secret:"false" port:"bind" component:"compose" desc:"Alamat bind host untuk port PostgreSQL (default loopback 127.0.0.1)" example:"127.0.0.1"`
	DBName             string        `env:"DB_NAME" type:"string" default:"kopdes_db" required:"all" secret:"false" component:"api,compose,scripts" desc:"Nama database target PostgreSQL (akhiran _test untuk DB uji)" example:"kopdes_test"`
	DBUser             string        `env:"DB_USER" type:"string" default:"kopdes_app" required:"all" secret:"false" component:"api,db-init,scripts" desc:"Role PostgreSQL non-superuser aplikasi" example:"kopdes_app"`
	DBPassword         string        `env:"DB_PASSWORD" type:"string" default:"" required:"all" secret:"true" component:"api,db-init" desc:"Kata sandi role DB_USER (wajib diset, kosong di .env.example)" example:"kopdes_test_setA_pass_123!"`
	DBSSLMode          string        `env:"DB_SSLMODE" type:"string" default:"disable" required:"all" secret:"false" component:"api,scripts" desc:"Mode SSL PostgreSQL (require/verify-full di production)" example:"disable"`
	DatabaseURL        string        `env:"DATABASE_URL" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Opsional DSN lengkap; jika diset wajib konsisten dengan komponen DB_*" example:""`
	DBAppRole          string        `env:"DB_APP_ROLE" type:"string" default:"kopdes_app" required:"all" secret:"false" component:"api,db-init" desc:"Nama role non-superuser untuk SET ROLE dan guard startup" example:"kopdes_app"`
	PostgresInitUser   string        `env:"POSTGRES_USER" type:"string" default:"postgres" required:"compose" secret:"false" component:"compose" desc:"User bootstrap kontainer PostgreSQL (init only)" example:"postgres"`
	PostgresInitPassword string      `env:"POSTGRES_PASSWORD" type:"string" default:"" required:"compose" secret:"true" component:"compose" desc:"Sandi bootstrap superuser PostgreSQL kontainer (init only)" example:"pg_test_bootstrap_123!"`
	KopdesAppPassword  string        `env:"KOPDES_APP_PASSWORD" type:"string" default:"" required:"db-init" secret:"true" component:"db-init" desc:"Sandi role kopdes_app saat inisialisasi db/docker-init/01_roles.sql" example:"kopdes_test_setA_pass_123!"`
	AllowSuperuserDB   bool          `env:"ALLOW_SUPERUSER_DB" type:"bool" default:"false" required:"optional" secret:"false" component:"api" desc:"Hanya untuk diagnosa lokal; wajib false di production" example:"false"`
	DBPoolMaxConns     int           `env:"DB_POOL_MAX_CONNS" type:"int" default:"25" required:"optional" secret:"false" component:"api" desc:"Batas maksimum koneksi pool pgx" example:"25"`
	DBPoolMinConns     int           `env:"DB_POOL_MIN_CONNS" type:"int" default:"2" required:"optional" secret:"false" component:"api" desc:"Batas minimum koneksi pool pgx" example:"2"`
	DBStatementTimeout time.Duration `env:"DB_STATEMENT_TIMEOUT" type:"duration" default:"15s" required:"optional" secret:"false" component:"api" desc:"Timeout eksekusi query PostgreSQL per statement" example:"15s"`
	DBLockTimeout      time.Duration `env:"DB_LOCK_TIMEOUT" type:"duration" default:"5s" required:"optional" secret:"false" component:"api" desc:"Timeout menunggu kunci PostgreSQL" example:"5s"`
	DBIdleInTxTimeout  time.Duration `env:"DB_IDLE_IN_TX_TIMEOUT" type:"duration" default:"30s" required:"optional" secret:"false" component:"api" desc:"Timeout transaksi idle PostgreSQL" example:"30s"`

	// --- Kriptografi & Autentikasi ---
	JWTSecret            string        `env:"JWT_SECRET" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Kunci HMAC penandatangan JWT aktif (min 32 karakter)" example:"test_jwt_secret_setA_0123456789abcdef0123456789abcdef"`
	JWTPreviousSecret    string        `env:"JWT_PREVIOUS_SECRET" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Kunci JWT sebelumnya saat rotasi (hanya untuk verifikasi)" example:""`
	JWTAlgorithm         string        `env:"JWT_ALGORITHM" type:"string" default:"HS256" required:"all" secret:"false" component:"api" desc:"Algoritma HMAC JWT yang diizinkan (HS256|HS384|HS512)" example:"HS256"`
	JWTIssuer            string        `env:"JWT_ISSUER" type:"string" default:"kopdes-merah-putih" required:"all" secret:"false" component:"api" desc:"Nilai klaim iss pada JWT" example:"kopdes-merah-putih"`
	JWTAccessTTL         time.Duration `env:"JWT_ACCESS_TTL" type:"duration" default:"15m" required:"all" secret:"false" component:"api" desc:"Masa berlaku access token JWT" example:"15m"`
	JWTRefreshTTL        time.Duration `env:"JWT_REFRESH_TTL" type:"duration" default:"168h" required:"all" secret:"false" component:"api" desc:"Masa berlaku refresh token berotasi" example:"168h"`
	AESMasterKey         string        `env:"AES_MASTER_KEY" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Kunci utama AES-256-GCM enkripsi PII (min 32 byte/hex64)" example:"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"`
	AESKeyVersion        string        `env:"AES_KEY_VERSION" type:"string" default:"v1" required:"all" secret:"false" component:"api" desc:"Prefix versi kunci aktif untuk ciphertext AES-GCM" example:"v1"`
	AESKeyID             string        `env:"AES_KEY_ID" type:"string" default:"k1" required:"all" secret:"false" component:"api" desc:"ID kunci aktif untuk rotasi kunci AES-GCM" example:"k1"`
	AESPreviousKeys      string        `env:"AES_PREVIOUS_KEYS" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Kunci AES lama format 'v1:k1=hex;v1:k2=hex' untuk dekripsi data historis saat rotasi" example:""`
	BlindIndexSecret     string        `env:"BLIND_INDEX_SECRET" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Rahasia root HKDF-SHA256 untuk blind index NIK/email" example:"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"`
	QRISSecret           string        `env:"QRIS_SECRET" type:"string" default:"" required:"all" secret:"true" component:"api" desc:"Kunci HMAC penandatanganan payload QRIS internal" example:"11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"`
	MetricsToken         string        `env:"METRICS_TOKEN" type:"string" default:"" required:"all" secret:"true" component:"api,prometheus" desc:"Bearer token wajib (>= 32 byte) untuk endpoint /metrics" example:"metrics_token_setA_0123456789abcdef0123456789abcdef"`
	MetricsPreviousToken string        `env:"METRICS_PREVIOUS_TOKEN" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Token /metrics sebelumnya selama transisi rotasi" example:""`
	BcryptCost           int           `env:"BCRYPT_COST" type:"int" default:"12" required:"all" secret:"false" component:"api" desc:"Biaya komputasi bcrypt hashing kata sandi (min 10 di prod)" example:"12"`

	// --- MFA, Rate Limit, & HTTP Hardening ---
	MFAIssuer             string        `env:"MFA_ISSUER" type:"string" default:"Kopdes Merah Putih" required:"all" secret:"false" component:"api" desc:"Nama issuer yang tampil pada aplikasi autentikator TOTP" example:"Kopdes Merah Putih"`
	MFAMaxAttempts        int           `env:"MFA_MAX_ATTEMPTS" type:"int" default:"3" required:"all" secret:"false" component:"api" desc:"Batas percobaan salah per mfa_session_token" example:"3"`
	MFASessionTTL         time.Duration `env:"MFA_SESSION_TTL" type:"duration" default:"5m" required:"all" secret:"false" component:"api" desc:"Masa berlaku sesi tantangan MFA setelah password valid" example:"5m"`
	MFASetupTTL           time.Duration `env:"MFA_SETUP_TTL" type:"duration" default:"10m" required:"all" secret:"false" component:"api" desc:"Masa berlaku token pendaftaran MFA akun istimewa" example:"10m"`
	LoginMaxAttempts      int           `env:"LOGIN_MAX_ATTEMPTS" type:"int" default:"5" required:"all" secret:"false" component:"api" desc:"Batas kegagalan login sebelum IP/akun dikunci sementara" example:"5"`
	LoginLockoutDuration  time.Duration `env:"LOGIN_LOCKOUT_DURATION" type:"duration" default:"15m" required:"all" secret:"false" component:"api" desc:"Durasi penguncian login setelah batas terlampaui" example:"15m"`
	RateLimitRPS          float64       `env:"RATE_LIMIT_RPS" type:"float64" default:"30" required:"optional" secret:"false" component:"api" desc:"Token bucket rate limit request per detik per IP" example:"30"`
	RateLimitBurst        int           `env:"RATE_LIMIT_BURST" type:"int" default:"60" required:"optional" secret:"false" component:"api" desc:"Burst maksimum rate limiter HTTP per IP" example:"60"`
	RateLimitMaxClients   int           `env:"RATE_LIMIT_MAX_CLIENTS" type:"int" default:"10000" required:"optional" secret:"false" component:"api" desc:"Batas entri memori pelacak rate limiter" example:"10000"`
	MaxBodyBytes          int64         `env:"MAX_BODY_BYTES" type:"int64" default:"10485760" required:"optional" secret:"false" component:"api" desc:"Batas ukuran request body HTTP (default 10MB)" example:"10485760"`
	HTTPReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" type:"duration" default:"5s" required:"optional" secret:"false" component:"api" desc:"Timeout membaca header HTTP (anti-Slowloris)" example:"5s"`
	HTTPReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" type:"duration" default:"10s" required:"optional" secret:"false" component:"api" desc:"Timeout membaca seluruh request HTTP" example:"10s"`
	HTTPWriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT" type:"duration" default:"15s" required:"optional" secret:"false" component:"api" desc:"Timeout menulis respons HTTP" example:"15s"`
	HTTPIdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" type:"duration" default:"60s" required:"optional" secret:"false" component:"api" desc:"Timeout koneksi HTTP keep-alive idle" example:"60s"`

	// --- Pekerja Latar, Partisi, Retensi PDP, & Fitur Pembayaran ---
	PartitionMonthsAhead         int           `env:"PARTITION_MONTHS_AHEAD" type:"int" default:"3" required:"optional" secret:"false" component:"api" desc:"Jumlah bulan partisi ke depan yang dijaga selalu tersedia" example:"3"`
	PartitionJobInterval         time.Duration `env:"PARTITION_JOB_INTERVAL" type:"duration" default:"24h" required:"optional" secret:"false" component:"api" desc:"Interval eksekusi job pemeliharaan partisi bulanan" example:"24h"`
	PDPRejectedRetentionDays     int           `env:"PDP_REJECTED_RETENTION_DAYS" type:"int" default:"30" required:"optional" secret:"false" component:"api" desc:"Retensi hari berkas pendaftaran ditolak sebelum purge (PERLU VERIFIKASI HUKUM)" example:"30"`
	PDPPurgeInterval             time.Duration `env:"PDP_PURGE_INTERVAL" type:"duration" default:"24h" required:"optional" secret:"false" component:"api" desc:"Interval job pembersihan pendaftaran ditolak" example:"24h"`
	QRISDynamicEnabled           bool          `env:"QRIS_DYNAMIC_ENABLED" type:"bool" default:"false" required:"optional" secret:"false" component:"api" desc:"Feature flag QRIS dinamis (false = QRIS statis + kode unik)" example:"false"`
	AllowPersonalPaymentPilot    bool          `env:"ALLOW_PERSONAL_PAYMENT_PILOT" type:"bool" default:"false" required:"optional" secret:"false" component:"api" desc:"Mengizinkan rekening pilot perorangan terbatas dengan batas harian" example:"false"`
	PilotDefaultMaxPerTx         int64         `env:"PILOT_DEFAULT_MAX_PER_TX" type:"int64" default:"500000" required:"optional" secret:"false" component:"api" desc:"Batas nominal default per transaksi rekening pilot (Rupiah)" example:"500000"`
	PilotDefaultMaxPerDay        int64         `env:"PILOT_DEFAULT_MAX_PER_DAY" type:"int64" default:"2000000" required:"optional" secret:"false" component:"api" desc:"Batas nominal akumulasi harian rekening pilot Asia/Jakarta (Rupiah)" example:"2000000"`
	DualApprovalDefaultThreshold int64         `env:"DUAL_APPROVAL_DEFAULT_THRESHOLD" type:"int64" default:"500000" required:"optional" secret:"false" component:"api" desc:"Ambang nominal default kewajiban persetujuan ganda (Rupiah)" example:"500000"`

	// --- Penyimpanan, Backup, Notifikasi & Uji ---
	StorageKYCDir        string `env:"STORAGE_KYC_DIR" type:"string" default:"./storage/kyc" required:"all" secret:"false" component:"api,compose" desc:"Direktori penyimpanan berkas KYC terenkripsi/lokal" example:"./storage/kyc"`
	MaxKYCUploadBytes    int64  `env:"MAX_KYC_UPLOAD_BYTES" type:"int64" default:"5242880" required:"optional" secret:"false" component:"api" desc:"Batas ukuran maksimal satu berkas KYC (byte)" example:"5242880"`
	BackupDir            string `env:"BACKUP_DIR" type:"string" default:"./backups" required:"scripts" secret:"false" component:"scripts" desc:"Direktori penyimpanan hasil backup pg_dump/basebackup" example:"./backups"`
	BackupEncryptionKey  string `env:"BACKUP_ENCRYPTION_KEY" type:"string" default:"" required:"prod" secret:"true" component:"scripts" desc:"Passphrase OpenSSL AES-256-CBC untuk enkripsi arsip backup" example:"test_backup_key_setA_9876543210!"`
	BackupRetentionDays  int    `env:"BACKUP_RETENTION_DAYS" type:"int" default:"30" required:"scripts" secret:"false" component:"scripts" desc:"Jumlah hari retensi file backup lokal" example:"30"`
	NotifierProvider     string `env:"NOTIFIER_PROVIDER" type:"string" default:"noop" required:"optional" secret:"false" component:"api" desc:"Adapter pengirim notifikasi outbox (noop|webhook)" example:"noop"`
	WhatsAppAPIKey       string `env:"WHATSAPP_API_KEY" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Kunci API gateway WhatsApp opsional untuk worker outbox" example:""`
	SMSAPIKey            string `env:"SMS_API_KEY" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Kunci API gateway SMS opsional untuk worker outbox" example:""`
	WebhookSigningSecret string `env:"WEBHOOK_SIGNING_SECRET" type:"string" default:"" required:"optional" secret:"true" component:"api" desc:"Rahasia HMAC-SHA256 penandatangan request outbox webhook" example:""`
	TestDatabaseURL      string `env:"TEST_DATABASE_URL" type:"string" default:"" required:"test" secret:"true" component:"tests" desc:"DSN khusus suite tes Go (wajib berakhiran *_test)" example:"postgres://kopdes_app:kopdes_test_setA_pass_123!@127.0.0.1:5433/kopdes_test?sslmode=disable"`
}

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

		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(raw)
		case reflect.Int:
			if raw == "" {
				continue
			}
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return cfg, fmt.Errorf("konfigurasi %s tidak valid (%q): harus bilangan bulat", envKey, raw)
			}
			fv.SetInt(int64(parsed))
		case reflect.Int64:
			if fv.Type() == reflect.TypeOf(time.Duration(0)) {
				if raw == "" {
					continue
				}
				d, err := time.ParseDuration(raw)
				if err != nil {
					return cfg, fmt.Errorf("konfigurasi %s tidak valid (%q): harus format durasi Go (mis. 15m)", envKey, raw)
				}
				fv.SetInt(int64(d))
			} else {
				if raw == "" {
					continue
				}
				parsed, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					return cfg, fmt.Errorf("konfigurasi %s tidak valid (%q): harus int64", envKey, raw)
				}
				fv.SetInt(parsed)
			}
		case reflect.Float64:
			if raw == "" {
				continue
			}
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return cfg, fmt.Errorf("konfigurasi %s tidak valid (%q): harus angka desimal", envKey, raw)
			}
			fv.SetFloat(parsed)
		case reflect.Bool:
			if raw == "" {
				continue
			}
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return cfg, fmt.Errorf("konfigurasi %s tidak valid (%q): harus true/false", envKey, raw)
			}
			fv.SetBool(parsed)
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.String {
				var items []string
				for _, part := range strings.Split(raw, ",") {
					trimmed := strings.TrimSpace(part)
					if trimmed != "" {
						items = append(items, trimmed)
					}
				}
				fv.Set(reflect.ValueOf(items))
			}
		}
	}

	if err := cfg.validateBasic(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// LoadFromEnv memuat konfigurasi menggunakan os.LookupEnv.
func LoadFromEnv() (AppConfig, error) {
	return Load(os.LookupEnv)
}

func (c AppConfig) validateBasic() error {
	switch strings.ToUpper(strings.TrimSpace(c.JWTAlgorithm)) {
	case "HS256", "HS384", "HS512":
	default:
		return fmt.Errorf("JWT_ALGORITHM tidak didukung: %q (izinkan HS256, HS384, atau HS512)", c.JWTAlgorithm)
	}

	if c.APIPort <= 0 || c.APIPort > 65535 {
		return fmt.Errorf("API_PORT di luar jangkauan: %d", c.APIPort)
	}
	if c.MetricsPort < 0 || c.MetricsPort > 65535 {
		return fmt.Errorf("METRICS_PORT di luar jangkauan: %d", c.MetricsPort)
	}
	if c.DBPort <= 0 || c.DBPort > 65535 {
		return fmt.Errorf("DB_PORT di luar jangkauan: %d", c.DBPort)
	}
	if c.BcryptCost < 4 || c.BcryptCost > 31 {
		return fmt.Errorf("BCRYPT_COST harus antara 4 dan 31, dapat %d", c.BcryptCost)
	}
	if c.MFAMaxAttempts <= 0 {
		return fmt.Errorf("MFA_MAX_ATTEMPTS harus > 0, dapat %d", c.MFAMaxAttempts)
	}
	if c.LoginMaxAttempts <= 0 {
		return fmt.Errorf("LOGIN_MAX_ATTEMPTS harus > 0, dapat %d", c.LoginMaxAttempts)
	}
	if c.PartitionMonthsAhead <= 0 {
		return fmt.Errorf("PARTITION_MONTHS_AHEAD harus > 0, dapat %d", c.PartitionMonthsAhead)
	}
	if c.PDPRejectedRetentionDays <= 0 {
		return fmt.Errorf("PDP_REJECTED_RETENTION_DAYS harus > 0, dapat %d", c.PDPRejectedRetentionDays)
	}
	if c.AppTimezone != "" {
		if _, err := time.LoadLocation(c.AppTimezone); err != nil {
			return fmt.Errorf("APP_TIMEZONE tidak dikenal %q: %w", c.AppTimezone, err)
		}
	}

	// Validasi konflik DATABASE_URL tanpa pernah membocorkan sandi
	if strings.TrimSpace(c.DatabaseURL) != "" {
		if err := c.validateDatabaseURLConsistency(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateProduction memvalidasi kesiapan konfigurasi ketat untuk lingkungan produksi.
func (c AppConfig) ValidateProduction() error {
	if c.AllowSuperuserDB {
		return fmt.Errorf("ALLOW_SUPERUSER_DB wajib false di produksi")
	}
	if strings.EqualFold(c.DBSSLMode, "disable") {
		return fmt.Errorf("DB_SSLMODE tidak boleh disable di produksi")
	}
	if len(c.MetricsToken) < 32 {
		return fmt.Errorf("METRICS_TOKEN wajib minimal 32 byte")
	}
	if c.BcryptCost < 10 {
		return fmt.Errorf("BCRYPT_COST wajib minimal 10 di produksi")
	}
	for _, o := range c.AllowedOrigins {
		if o == "*" {
			return fmt.Errorf("ALLOWED_ORIGINS tidak boleh berisi wildcard '*'")
		}
	}
	if strings.TrimSpace(c.DBPassword) == "" {
		return fmt.Errorf("DB_PASSWORD wajib diisi di produksi")
	}
	return nil
}

// BuildDatabaseDSN merakit DSN PostgreSQL secara deterministik dari DB_HOST, DB_PORT,
// DB_NAME, DB_USER, DB_PASSWORD, dan DB_SSLMODE. Jika DATABASE_URL ikut diset,
// sistem memverifikasi kesesuaian penuh dan gagal jika bertentangan tanpa mencetak password.
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

// Catalog mengembalikan daftar spesifikasi variabel environment secara otomatis dari tag struct AppConfig.
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
	b.WriteString("> **Dokumen ini di-generate otomatis** dari tag struct `AppConfig` di `internal/config/config.go`.\n")
	b.WriteString("> Jangan mengedit tabel secara manual; ubah tag struct `AppConfig` lalu jalankan generator.\n\n")

	b.WriteString("## 1. Pemisahan Konfigurasi: Environment vs Database (`konfigurasi_aturan`)\n\n")
	b.WriteString("| Jenis Pengaturan | Lokasi Penyimpanan | Contoh Parameter |\n")
	b.WriteString("|---|---|---|\n")
	b.WriteString("| Infrastruktur, Port, DSN, Kunci Kriptografi, Token, Batas Global API | Variabel Environment (`.env`) | `API_PORT`, `DB_*`, `JWT_SECRET`, `AES_MASTER_KEY`, `METRICS_TOKEN`, `BCRYPT_COST` |\n")
	b.WriteString("| Kebijakan Bisnis per Koperasi / Tenant | Tabel Database `konfigurasi_aturan` (RLS per `koperasi_id`) | Ambang persetujuan ganda khusus tenant, `auto_approve_mutasi_enabled`, batas pilot koperasi, jadwal iuran |\n\n")
	b.WriteString("Nilai seperti `PILOT_DEFAULT_MAX_PER_TX` atau `DUAL_APPROVAL_DEFAULT_THRESHOLD` di `.env` hanya berfungsi sebagai **nilai default sistem** ketika tenant belum menimpanya di tabel `konfigurasi_aturan`.\n\n")

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
