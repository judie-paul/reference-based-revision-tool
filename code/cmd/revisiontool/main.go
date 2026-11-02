// Command revisiontool runs the document-grounded exam revision web app.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"revisiontool/internal/config"
	"revisiontool/internal/llm"
	"revisiontool/internal/search"
	"revisiontool/internal/store"
	"revisiontool/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer st.Close()

	// The server still starts without an API key so the user can see what
	// to configure; uploads stay disabled until a key is set.
	client, llmErr := llm.New(cfg)
	if llmErr != nil {
		log.Printf("WARNING: %v", llmErr)
	} else {
		log.Printf("LLM: %s (context window %d tokens)", client.Model(), cfg.LLMContextWindow)
	}
	sp := search.New(cfg)
	if sp.Available() {
		log.Printf("External search: %s", sp.Name())
	} else {
		log.Printf("External search: not configured (set TAVILY_API_KEY to enable)")
	}

	srv, err := web.New(cfg, st, client, llmErr, sp)
	if err != nil {
		log.Fatalf("loading templates: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,  // large uploads on slow links
		WriteTimeout:      15 * time.Minute, // answer generation can take a while
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		log.Printf("Open http://%s in your browser", displayAddr(cfg.Addr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)
}

func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "localhost" + addr
	}
	return addr
}
