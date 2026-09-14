package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	BotToken           string
	DBDriver           string
	DBPath             string
	PostgresDSN        string
	ResponsibleIDs     []int64
	AdminIDs           []int64
	AdminPassword      string
	DefaultStage       string
	ArchiveDir         string
	MatWords           []string
	CAFile             string
	InsecureSkipVerify bool
}

func Load() (*Config, error) {
	cfg := &Config{
		BotToken:           os.Getenv("BOT_TOKEN"),
		DBPath:             os.Getenv("DB_PATH"),
		PostgresDSN:        os.Getenv("DATABASE_URL"),
		ArchiveDir:         envOr("ARCHIVE_DIR", "archive"),
		MatWords:           splitCSV(envOr("MAT_WORDS", defaultMatWords)),
		CAFile:             os.Getenv("CA_CERT_FILE"),
		InsecureSkipVerify: os.Getenv("INSECURE_SKIP_VERIFY") == "true",
	}

	if cfg.BotToken == "" {
		return nil, fmt.Errorf("BOT_TOKEN env is required")
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "local_db/postobot.db"
	}

	switch {
	case cfg.PostgresDSN != "":
		cfg.DBDriver = "postgres"
	case strings.HasPrefix(cfg.PostgresDSN, "postgres://"), strings.HasPrefix(cfg.PostgresDSN, "postgresql://"):
		cfg.DBDriver = "postgres"
	default:
		cfg.DBDriver = "sqlite"
	}

	ids, err := parseIDs(os.Getenv("RESPONSIBLE_IDS"))
	if err != nil {
		return nil, fmt.Errorf("RESPONSIBLE_IDS: %w", err)
	}
	cfg.ResponsibleIDs = ids

	adminIDs, err := parseIDs(os.Getenv("ADMIN_IDS"))
	if err != nil {
		return nil, fmt.Errorf("ADMIN_IDS: %w", err)
	}
	cfg.AdminIDs = adminIDs

	cfg.AdminPassword = os.Getenv("ADMIN_PASSWORD")
	cfg.DefaultStage = envOr("BOT_STAGE", StageBeta)

	return cfg, nil
}

// Stages the bot runs in.
const (
	StageBeta       = "beta"
	StagePublicBeta = "public_beta"
	StageFinal      = "final"
)

func ValidStage(s string) bool {
	switch s {
	case StageBeta, StagePublicBeta, StageFinal:
		return true
	}
	return false
}

func parseIDs(raw string) ([]int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var ids []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", part, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const defaultMatWords = "блядь,блять,хуй,хуя,пизда,пиздец,пидор,сука,ебал,ебать,ебан,нахуй,похуй,мудак,идиот,дурак,дебил,заебал,заеба"
