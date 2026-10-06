package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(ctx context.Context, dsn string) (*gorm.DB, *sql.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("initialize database: %w", err)
	}
	pool, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connect to database: %w", err)
	}
	return db, pool, nil
}
