package infradatabase

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"time"

	infraconfig "GCFeed/internal/infra/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
)

// New 创建 PostgreSQL 连接池，并用 Ping 提前验证配置是否可用。
func New(dbcfg infraconfig.DatabaseConfig) (*sql.DB, error) {
	query := url.Values{}
	query.Set("sslmode", dbcfg.SSLMode)
	query.Set("TimeZone", dbcfg.TimeZone)
	dsn := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(dbcfg.User, dbcfg.Password),
		Host:     net.JoinHostPort(dbcfg.Host, fmt.Sprintf("%d", dbcfg.Port)),
		Path:     dbcfg.Name,
		RawQuery: query.Encode(),
	}).String()

	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	db := stdlib.OpenDB(*connConfig, stdlib.OptionAfterConnect(configureUTCCodecs))

	// 小项目使用固定连接池参数，后续可按压测结果调整。
	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(50)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return db, nil
}

func configureUTCCodecs(_ context.Context, conn *pgx.Conn) error {
	conn.TypeMap().RegisterType(&pgtype.Type{
		Name:  "timestamp",
		OID:   pgtype.TimestampOID,
		Codec: &pgtype.TimestampCodec{ScanLocation: time.UTC},
	})
	conn.TypeMap().RegisterType(&pgtype.Type{
		Name:  "timestamptz",
		OID:   pgtype.TimestamptzOID,
		Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
	})
	return nil
}
