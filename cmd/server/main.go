package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api"
)

func main() {
	portStr := os.Getenv("PORT")
	port := 8080
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_DSN")
	}
	if dsn == "" {
		// Fallback ke port PostgreSQL 16 yang sedang aktif
		dsn = "postgres://kopdes_app:kopdes_dev_password@127.0.0.1:5433/kopdes_db?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgxCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("Gagal mem-parsing DSN PostgreSQL: %v", err)
	}

	pgxCfg.MaxConns = 30
	pgxCfg.MinConns = 5
	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE kopdes_app;")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		log.Fatalf("Gagal inisialisasi koneksi pool PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("PostgreSQL tidak dapat dihubungi: %v", err)
	}
	log.Printf("Koneksi ke PostgreSQL 16 berhasil.")

	cfg := api.DefaultConfig()
	cfg.Port = port
	if s := os.Getenv("JWT_SECRET"); s != "" {
		cfg.JWTSecret = s
	}
	if s := os.Getenv("BLIND_INDEX_SECRET"); s != "" {
		cfg.BlindIndexSecret = s
	}
	if s := os.Getenv("QRIS_SECRET"); s != "" {
		cfg.QRISSecret = s
	}

	server := api.NewServer(pool, cfg)

	// Channel sinyal shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server API Kopdes Merah Putih berjalan pada port :%d", port)
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server berhenti karena error: %v", err)
		}
	}()

	<-stop
	log.Println("Menerima sinyal shutdown, mematikan server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error saat shutdown server: %v", err)
	}
	fmt.Println("Server berhasil dimatikan secara bersih.")
}
