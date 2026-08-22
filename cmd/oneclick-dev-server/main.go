package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LuongVanDuy/oneclick-dev-server/internal/browser"
	"github.com/LuongVanDuy/oneclick-dev-server/internal/server"
	"github.com/LuongVanDuy/oneclick-dev-server/internal/web"
)

const listenAddress = "127.0.0.1:3765"

func main() {
	app, err := server.New(listenAddress, web.Assets())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("OneClick Dev Server: %s", app.URL())
	if err := browser.Open(app.URL()); err != nil {
		log.Printf("browser open skipped: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Serve()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received %s; shutting down", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
