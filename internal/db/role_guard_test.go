package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	internaldb "github.com/kopdes/merah-putih/internal/db"
)

func getDBPool(t *testing.T, user, password string) *pgxpool.Pool {
	t.Helper()
	if user == "postgres" && password == "" {
		password = internaldb.GetTestEnv("POSTGRES_PASSWORD")
	} else if password == "" {
		password = internaldb.GetTestEnv("DB_PASSWORD")
	}
	dsn, err := internaldb.ResolveTestDatabaseDSN(user, password)
	if err != nil {
		t.Fatalf("ResolveTestDatabaseDSN gagal untuk user %s: %v", user, err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig gagal untuk user %s: %v", user, err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Connect error for user %s: %v", user, err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("Ping error for user %s: %v", user, err)
	}
	return pool
}

func TestSuperuserRejectedOnStartup(t *testing.T) {
	ctx := context.Background()

	poolSuper := getDBPool(t, "postgres", internaldb.GetTestEnv("POSTGRES_PASSWORD"))
	defer poolSuper.Close()

	if err := internaldb.CheckDatabaseRole(ctx, poolSuper, false); err == nil {
		t.Fatalf("Harusnya GAGAL: Aplikasi tidak boleh mengizinkan superuser berjalan saat allowSuperuser = false")
	}
	if err := internaldb.CheckDatabaseRole(ctx, poolSuper, true); err != nil {
		t.Fatalf("Gagal: allowSuperuser=true harusnya diizinkan untuk dev, dapat: %v", err)
	}

	poolApp := getDBPool(t, "kopdes_app", internaldb.GetTestEnv("DB_PASSWORD"))
	defer poolApp.Close()
	if err := internaldb.CheckDatabaseRole(ctx, poolApp, false); err != nil {
		t.Fatalf("kopdes_app bukan superuser, harusnya lolos: %v", err)
	}
}
