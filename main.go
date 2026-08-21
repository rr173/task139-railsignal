// Command task139-railsignal serves the railway signal interlocking engine
// over HTTP, or runs the in-process smoke test when invoked with --smoke-test.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"task139-railsignal/internal/httpapi"
	"task139-railsignal/internal/selfcheck"
	"task139-railsignal/internal/store"
)

func main() {
	var (
		smoke bool
		addr  string
		db    string
	)
	flag.BoolVar(&smoke, "smoke-test", false, "run the in-process self-check and exit")
	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&db, "db", "", "SQLite database path (default a temp file)")
	flag.Parse()

	if smoke {
		if err := selfcheck.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "smoke-test FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("smoke-test PASSED")
		return
	}

	dbPath := db
	if dbPath == "" {
		dbPath = envOr("DB_PATH", "railsignal.db")
	}
	st, err := store.New(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	handler := httpapi.NewFromStore(st)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("railsignal engine listening on %s (db=%s)", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
