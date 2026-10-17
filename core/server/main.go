// Blizko broker: bind to loopback and expose through the existing HTTPS Tuna tunnel.
package main

import (
	"blizko/core/diagnostics"
	"blizko/core/mailbox"
	"blizko/core/transport"
	"blizko/core/webapp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:3340", "Loopback HTTP listen address behind HTTPS tunnel")
	dir := flag.String("data", "diagnostic-data", "Private diagnostic directory")
	token := flag.String("token", "", "Optional diagnostic token file; empty disables collection")
	initToken := flag.Bool("init", false, "Create token file without overwriting")
	webOrigin := flag.String("web-origin", "", "Exact HTTPS origin allowed for the web client; empty disables browser sockets")
	mailDir := flag.String("mailbox", "", "Private persistent encrypted queue and Web Push state directory")
	flag.Parse()
	if err := webapp.ValidateOrigin(*webOrigin); err != nil {
		log.Fatal(err)
	}
	if *initToken {
		b := make([]byte, 32)
		if _, e := rand.Read(b); e != nil {
			log.Fatal(e)
		}
		f, e := os.OpenFile(*token, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			log.Fatal(e)
		}
		_, e = f.WriteString(base64.RawURLEncoding.EncodeToString(b))
		if e != nil {
			f.Close()
			log.Fatal(e)
		}
		if e = f.Close(); e != nil {
			log.Fatal(e)
		}
		return
	}
	host, _, e := net.SplitHostPort(*listen)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("listen must be loopback; use an HTTPS tunnel")
	}
	listener, e := net.Listen("tcp", *listen)
	if e != nil {
		log.Fatal(e)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	broker := transport.NewServerWithBrowserOrigin(*webOrigin)
	if *mailDir != "" {
		store, err := mailbox.Open(*mailDir, strings.Split(*webOrigin, ",")[0])
		if err != nil {
			log.Fatal(err)
		}
		defer store.Close()
		broker.Durable = store
		go store.Run(ctx)
	}
	defer broker.Close()
	mux := http.NewServeMux()
	mux.Handle(transport.Path, broker)
	mux.Handle("/web/", webapp.Handler(*webOrigin))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/web/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(204)
	})
	if *token != "" {
		collector, err := diagnostics.Open(ctx, *dir, *token)
		if err != nil {
			log.Fatal(err)
		}
		mux.Handle(diagnostics.Path, collector)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	go func() { <-ctx.Done(); broker.Close(); server.Close() }()
	log.Print("Blizko server ready on loopback")
	if e = server.Serve(listener); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
