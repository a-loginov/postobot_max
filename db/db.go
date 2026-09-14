package db

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	*gorm.DB
}

func New(driver, path, dsn string) (*DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "postgres":
		dialector = postgres.Open(dsn)
	case "sqlite":
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db dir %s: %w", dir, err)
			}
		}
		dialector = sqlite.Open(path)
	default:
		return nil, fmt.Errorf("unknown db driver %q", driver)
	}

	g, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s db: %w", driver, err)
	}

	return &DB{g}, nil
}

func (d *DB) Migrate() error {
	return d.AutoMigrate(&Student{}, &Request{})
}
