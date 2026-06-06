package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	if schema == "" || schema == "public" {
		return nil
	}
	_, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize())
	if err != nil {
		return fmt.Errorf("create schema %s: %w", schema, err)
	}
	return nil
}

func Connect(ctx context.Context, databaseURL string, maxConns, minConns int32, maxIdleMinutes int, queryExecMode, dbSchema string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	if maxIdleMinutes <= 0 {
		maxIdleMinutes = 10
	}
	cfg.MaxConnIdleTime = time.Duration(maxIdleMinutes) * time.Minute
	if queryExecMode == "simple_protocol" {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}
	if dbSchema != "" {
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		if dbSchema == "public" {
			cfg.ConnConfig.RuntimeParams["search_path"] = "public"
		} else {
			cfg.ConnConfig.RuntimeParams["search_path"] = dbSchema + ",public"
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
