package repository

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/config"
)

// NewPool creates a production-ready pgx connection pool.
//
// Supabase (and most managed Postgres) expose a PgBouncer pooler. PgBouncer in
// transaction mode does NOT support prepared statements. pgx defaults to
// prepared statements, which causes intermittent query failures (often 500s).
// We force simple protocol so every query is self-contained.
func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	dbURL := cfg.DatabaseURL
	if cfg.IsProduction() && !strings.Contains(dbURL, "sslmode=") {
		if strings.Contains(dbURL, "?") {
			dbURL += "&sslmode=require"
		} else {
			dbURL += "?sslmode=require"
		}
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	// Critical for Supabase / PgBouncer transaction pooler
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// Stay small: Nest + Go share the same Supabase connection budget
	poolCfg.MaxConns = 8
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}


// NewWorkerPool is a smaller pool for background jobs so POS/API keep headroom.
func NewWorkerPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	dbURL := cfg.DatabaseURL
	if cfg.IsProduction() && !strings.Contains(dbURL, "sslmode=") {
		if strings.Contains(dbURL, "?") {
			dbURL += "&sslmode=require"
		} else {
			dbURL += "?sslmode=require"
		}
	}
	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	maxConns := int32(3)
	if v := strings.TrimSpace(os.Getenv("WORKER_MAX_CONNS")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 10 {
			maxConns = int32(n)
		}
	}
	poolCfg.MaxConns = maxConns
	poolCfg.MinConns = 0
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create worker pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
