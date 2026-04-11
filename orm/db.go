package orm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"hk_ipo/pkg/storage/gormmodel"
)

// DB is the global gorm entrypoint.
// Call Init() before using it.
var DB *gorm.DB

var initOnce sync.Once
var initErr error

// Init initializes the global gorm DB connection.
//
// Config priority:
// 1) HK_IPO_DB_DSN (full sqlite DSN)
// 2) HK_IPO_DB_PATH (sqlite db file path)
// 3) fallback default: ./sql/hk_ipo.db
func Init() error {
	initOnce.Do(func() {
		cfg := loadConfigFromEnv()
		dsn := cfg.DSN
		if dsn == "" {
			if cfg.Path != ":memory:" {
				if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
					initErr = fmt.Errorf("mkdir db dir: %w", err)
					return
				}
			}
			dsn = sqliteDSN(cfg.Path)
		}

		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			initErr = fmt.Errorf("gorm open: %w", err)
			return
		}
		sqlDB, err := db.DB()
		if err != nil {
			initErr = fmt.Errorf("db(): %w", err)
			return
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(0)

		if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			initErr = fmt.Errorf("enable sqlite foreign keys: %w", err)
			return
		}
		if err := migrate(db); err != nil {
			initErr = fmt.Errorf("migrate schema: %w", err)
			return
		}
		if err := sqlDB.Ping(); err != nil {
			initErr = fmt.Errorf("db ping: %w", err)
			return
		}

		DB = db
	})
	return initErr
}

type config struct {
	DSN  string
	Path string
}

func loadConfigFromEnv() config {
	return config{
		DSN:  os.Getenv("HK_IPO_DB_DSN"),
		Path: getenv("HK_IPO_DB_PATH", "./sql/hk_ipo.db"),
	}
}

func sqliteDSN(path string) string {
	if path == ":memory:" {
		return "file::memory:?cache=shared&_fk=1&_busy_timeout=5000"
	}
	if strings.Contains(path, "?") {
		return path
	}
	return fmt.Sprintf("%s?_fk=1&_busy_timeout=5000&_journal_mode=WAL", path)
}

func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&gormmodel.Stock{},
		&gormmodel.StockOffering{},
		&gormmodel.StockCompany{},
		&gormmodel.StockGreenShoe{},
		&gormmodel.StockGreyMarket{},
		&gormmodel.StockPerformance{},
		&gormmodel.StockRaiseMoney{},
		&gormmodel.Intermediary{},
		&gormmodel.StockIntermediary{},
		&gormmodel.StockCompanySecretary{},
		&gormmodel.StockMajorShareholder{},
		&gormmodel.StockUseOfProceeds{},
		&gormmodel.StockManagement{},
		&gormmodel.StockAllotmentSummary{},
		&gormmodel.StockAllotmentTier{},
		&gormmodel.StockRawSection{},
		&gormmodel.StockRawItem{},
	)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
