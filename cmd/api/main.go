package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hengk7401/checkinme-go-api/internal/config"
	"github.com/hengk7401/checkinme-go-api/internal/db"
	"github.com/hengk7401/checkinme-go-api/internal/httpapi"
	"github.com/hengk7401/checkinme-go-api/internal/services"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config error: %v", err)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns, cfg.DBMaxConnIdleMinutes)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer pool.Close()

	telegram := services.NewTelegramClient(cfg.TelegramBotToken, cfg.TelegramDefaultChatID)
	srv := httpapi.NewServer(cfg, pool, telegram)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("CheckinMe API running on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
