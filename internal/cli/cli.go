// Package cli implements command parsing and the thin Management API client.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/app"
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/management"
	"github.com/kmpoltorak/routeqtus/internal/neterror"
)

var Version = "dev"

func Run(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: routeqtus serve|manage|check|migrate|config|version|openapi")
	}
	command := args[0]
	if command == "version" {
		_, err := fmt.Fprintln(out, Version)
		return err
	}
	if command == "config" {
		return client(ctx, args[1:], out, stderr)
	}
	if command != "serve" && command != "manage" && command != "check" && command != "migrate" && command != "openapi" {
		return fmt.Errorf("unknown command %q", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("config", "bootstrap.yaml", "bootstrap YAML path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	c := bootstrap.Defaults()
	if command != "openapi" {
		var err error
		c, err = bootstrap.Load(*file)
		if err != nil {
			return err
		}
	}
	logger := app.Logger(c.Logging, stderr)
	if command == "check" {
		_, err := fmt.Fprintln(out, "bootstrap configuration is valid")
		return err
	}
	if command == "openapi" {
		compiler, err := app.Compiler(c, logger)
		if err != nil {
			return err
		}
		a := management.New(&configservice.Service{Compiler: compiler}, "", "", 1, 1, nil)
		_, err = out.Write(append(a.Contract(), '\n'))
		return err
	}
	return app.Run(ctx, command, c, logger)
}
func client(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: routeqtus config validate|apply|show|status|rollback")
	}
	op := args[0]
	fs := flag.NewFlagSet("config "+op, flag.ContinueOnError)
	fs.SetOutput(stderr)
	address := fs.String("api", "http://127.0.0.1:9080", "Management API base URL")
	tokenEnv := fs.String("token-env", "ROUTEQTUS_MANAGEMENT_TOKEN", "environment reference for the bearer token")
	file := fs.String("file", "-", "configuration JSON file (- reads stdin)")
	generation := fs.Int64("generation", -1, "expected generation for mutation")
	revision := fs.Int64("revision", 0, "historical revision for rollback")
	effective := fs.Bool("effective", false, "show effective values, unsuitable for round-trip export")
	description := fs.String("description", "", "declared publication description")
	author := fs.String("author", "", "declared author, not authenticated identity")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	u, err := url.Parse(*address)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || !bootstrap.Loopback(u.Host))) {
		return fmt.Errorf("API URL must use HTTPS or a loopback HTTP address with explicit port")
	}
	token := os.Getenv(*tokenEnv)
	if token == "" {
		return fmt.Errorf("management token environment reference unavailable")
	}
	method, path := "GET", "/v1/config"
	var body []byte
	switch op {
	case "show":
		if *effective {
			path += "/effective"
		}
	case "status":
		path = "/v1/status"
	case "apply", "validate":
		method = "PUT"
		if op == "validate" {
			method = "POST"
			path += "/validate"
		}
		var reader io.Reader = os.Stdin
		var f *os.File
		if *file != "-" {
			f, err = os.Open(*file)
			if err != nil {
				return err
			}
			defer f.Close()
			reader = f
		}
		body, err = io.ReadAll(io.LimitReader(reader, (64<<20)+1))
		if err != nil {
			return err
		}
		if len(body) > 64<<20 {
			return fmt.Errorf("configuration document exceeds 64 MiB")
		}
	case "rollback":
		if *revision < 1 {
			return fmt.Errorf("--revision must be positive")
		}
		method = "POST"
		path = "/v1/rollback"
		body, _ = json.Marshal(map[string]int64{"revision_id": *revision})
	default:
		return fmt.Errorf("unknown config command %q", op)
	}
	if (op == "apply" || op == "rollback") && *generation < 0 {
		return fmt.Errorf("--generation is required; use config status first")
	}
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if *generation >= 0 {
		req.Header.Set("If-Match", management.ETag(*generation))
	}
	req.Header.Set("X-Revision-Description", *description)
	req.Header.Set("X-Revision-Author", *author)
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Management API request failed: %s", neterror.Reason(err))
	}
	defer res.Body.Close()
	if _, err = io.Copy(out, io.LimitReader(res.Body, 64<<20)); err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("Management API returned HTTP %s", strconv.Itoa(res.StatusCode))
	}
	return nil
}
