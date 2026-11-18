package server

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Config is read from PIDIEF_* environment variables.
type Config struct {
	Addr            string
	APIKeys         []string
	MaxRequestBytes int64
	Concurrency     int
	QueueSize       int
	QueueTimeout    time.Duration
	MaxTimeout      time.Duration
	AllowExternal   bool
	JobTTL          time.Duration
	MaxJobs         int
	MetricsPublic   bool
	ChromiumPath    string
	NoSandbox       bool
	RestartAfter    int
	RasterWorkers   int
	ShutdownTimeout time.Duration
}

// EnvVar documents one environment variable (README and `pidief serve --help`).
type EnvVar struct{ Name, Default, Help string }

// EnvVars lists every setting.
var EnvVars = []EnvVar{
	{"PIDIEF_ADDR", ":3000", "listen address"},
	{"PIDIEF_API_KEYS", "", "comma-separated API keys accepted in X-Api-Key; empty disables authentication (development only)"},
	{"PIDIEF_MAX_REQUEST_MB", "50", "maximum request body size in MB"},
	{"PIDIEF_CONCURRENCY", "min(4, CPUs)", "operations running at the same time"},
	{"PIDIEF_QUEUE_SIZE", "100", "requests that may wait for a free slot; more get 429"},
	{"PIDIEF_QUEUE_TIMEOUT", "30s", "how long a request waits for a slot before 503"},
	{"PIDIEF_MAX_TIMEOUT", "120s", "upper bound of the per-request timeout parameter"},
	{"PIDIEF_ALLOW_EXTERNAL", "false", "allow the allowExternal parameter (HTML may load internet resources)"},
	{"PIDIEF_JOB_TTL", "15m", "how long finished async job results are kept in memory"},
	{"PIDIEF_MAX_JOBS", "200", "maximum async jobs kept in memory"},
	{"PIDIEF_METRICS_PUBLIC", "true", "serve /metrics without an API key"},
	{"PIDIEF_CHROMIUM_PATH", "auto", "Chromium/Chrome executable"},
	{"PIDIEF_CHROMIUM_NO_SANDBOX", "false", "start Chromium with --no-sandbox (needed in most containers; the image sets true)"},
	{"PIDIEF_CHROMIUM_RESTART_AFTER", "200", "restart Chromium after this many conversions (bounds memory growth)"},
	{"PIDIEF_RASTER_WORKERS", "2", "PDFium WebAssembly instances for render/text"},
	{"PIDIEF_SHUTDOWN_TIMEOUT", "30s", "graceful shutdown wait"},
	{"PIDIEF_LOG_LEVEL", "info", "debug, info, warn, error"},
	{"PIDIEF_LOG_FORMAT", "json", "json or text"},
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// LoadConfig reads the environment.
func LoadConfig() (Config, error) {
	var errs []string
	intv := func(name string, def int) int {
		v, err := strconv.Atoi(env(name, strconv.Itoa(def)))
		if err != nil || v < 0 {
			errs = append(errs, name+" must be a non-negative integer")
		}
		return v
	}
	durv := func(name, def string) time.Duration {
		v, err := time.ParseDuration(env(name, def))
		if err != nil || v <= 0 {
			errs = append(errs, name+" must be a duration like 30s")
		}
		return v
	}
	boolv := func(name string, def bool) bool {
		v, err := strconv.ParseBool(env(name, strconv.FormatBool(def)))
		if err != nil {
			errs = append(errs, name+" must be true or false")
		}
		return v
	}
	c := Config{
		Addr:            env("PIDIEF_ADDR", ":3000"),
		MaxRequestBytes: int64(intv("PIDIEF_MAX_REQUEST_MB", 50)) << 20,
		Concurrency:     intv("PIDIEF_CONCURRENCY", min(4, runtime.NumCPU())),
		QueueSize:       intv("PIDIEF_QUEUE_SIZE", 100),
		QueueTimeout:    durv("PIDIEF_QUEUE_TIMEOUT", "30s"),
		MaxTimeout:      durv("PIDIEF_MAX_TIMEOUT", "120s"),
		AllowExternal:   boolv("PIDIEF_ALLOW_EXTERNAL", false),
		JobTTL:          durv("PIDIEF_JOB_TTL", "15m"),
		MaxJobs:         intv("PIDIEF_MAX_JOBS", 200),
		MetricsPublic:   boolv("PIDIEF_METRICS_PUBLIC", true),
		ChromiumPath:    os.Getenv("PIDIEF_CHROMIUM_PATH"),
		NoSandbox:       boolv("PIDIEF_CHROMIUM_NO_SANDBOX", false),
		RestartAfter:    intv("PIDIEF_CHROMIUM_RESTART_AFTER", 200),
		RasterWorkers:   intv("PIDIEF_RASTER_WORKERS", 2),
		ShutdownTimeout: durv("PIDIEF_SHUTDOWN_TIMEOUT", "30s"),
	}
	for _, k := range strings.Split(os.Getenv("PIDIEF_API_KEYS"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			c.APIKeys = append(c.APIKeys, k)
		}
	}
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("configuration: %s", strings.Join(errs, "; "))
	}
	return c, nil
}
