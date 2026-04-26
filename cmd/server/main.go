//go:build !wails

// Command frpdeck-server is the headless HTTP entry point for FrpDeck.
//
// It boots the SQLite store, the in-process frp client driver, the
// lifecycle manager, the auth + captcha + ntfy infrastructure, and
// finally the Gin router. The Wails desktop entry point lives behind
// `-tags wails` and reuses the same bootstrap function in this
// package.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	rt, err := bootstrap()
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	httpSrv := startHTTP(rt)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down (frp tunnels retained for next boot)")

	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
	rt.Close()
}
