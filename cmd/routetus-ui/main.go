package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/ui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "routetus-ui:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("routetus-ui", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:9180", "GUI listener; use a loopback-published port in Docker")
	address := fs.String("api", "http://127.0.0.1:9080", "Management API base URL")
	public := fs.String("origin", "", "Browser origin; required when binding a container interface")
	tokenEnv := fs.String("token-env", "ROUTETUS_MANAGEMENT_TOKEN", "Environment reference for the Management API token")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if !bootstrap.EnvName(*tokenEnv) {
		return fmt.Errorf("invalid token environment reference")
	}
	if *public == "" && !bootstrap.Loopback(*listen) {
		return fmt.Errorf("non-loopback container binding requires an explicit loopback --origin and a loopback-published port")
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	if *public == "" {
		*public = "http://" + listener.Addr().String()
	}
	origin, err := url.Parse(*public)
	if err != nil || origin.Scheme != "http" || !bootstrap.Loopback(origin.Host) || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return fmt.Errorf("GUI origin must be an HTTP URL with a numeric loopback address and port")
	}
	handler, err := ui.New(*address, os.Getenv(*tokenEnv), origin.Host)
	if err != nil {
		return err
	}
	defer handler.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	fmt.Fprintln(os.Stderr, "Routetus UI listening on", *public)
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := server.Shutdown(drain)
		if err != nil {
			_ = server.Close()
		}
		<-finished
		return err
	}
}
