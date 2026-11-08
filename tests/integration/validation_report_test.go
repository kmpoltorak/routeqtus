//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kmpoltorak/routeqtus/internal/app"
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/config"
	"github.com/kmpoltorak/routeqtus/internal/configservice"
	"github.com/kmpoltorak/routeqtus/internal/management"
	"github.com/kmpoltorak/routeqtus/internal/postgres"
	"github.com/kmpoltorak/routeqtus/internal/testutil"
)

func TestValidationReportAcrossAPIStatusAndLog(t *testing.T) {
	ctx := context.Background()
	s := database(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	compiler := testutil.Compiler(t)
	svc := &configservice.Service{Store: s, Compiler: compiler}
	good := testutil.Config(backend.URL)
	if _, err := svc.Publish(ctx, good, 0, postgres.Metadata{Principal: "report-test"}); err != nil {
		t.Fatal(err)
	}
	listener, err := s.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close(ctx)
	boot := bootstrap.Defaults()
	boot.Postgres.DSNEnv = "ROUTEQTUS_INTEGRATION_DSN"
	boot.Server.Listen, boot.Admin.Listen = freeAddress(t), freeAddress(t)
	boot.Sync.Notifications = false
	boot.Sync.PollInterval = "20ms"
	boot.Sync.MaxStaleness = "1m"
	logPath := filepath.Join(t.TempDir(), "runtime.jsonl")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	runctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = app.Run(runctx, "serve", boot, slog.New(slog.NewJSONHandler(log, nil)))
	}()
	stopProcess := func() {
		cancel()
		<-done
		if runErr != nil {
			t.Error(runErr)
		}
	}
	defer stopProcess()
	client := &http.Client{Timeout: time.Second}
	eventually(t, 2*time.Second, func() bool {
		res, e := client.Get("http://" + boot.Server.Listen)
		if e != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == 204
	})
	bad := testutil.Config(backend.URL)
	bad.Routes = make([]config.Route, 10000)
	for i := range bad.Routes {
		bad.Routes[i] = config.Route{ID: fmt.Sprintf("bad-%d", i), Path: "/bad//path", Upstream: "demo", Handlers: []string{}}
	}
	data, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 32)
	api := management.New(svc, token, "report-test", 4<<20, 8, nil)
	req := httptest.NewRequest("PUT", "/v1/config", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", `"g-1"`)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	var problem struct {
		Fields []config.FieldError `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if w.Code != 422 || len(problem.Fields) != 101 || w.Body.Len() > 64<<10 {
		t.Fatalf("status=%d fields=%d bytes=%d", w.Code, len(problem.Fields), w.Body.Len())
	}
	summary := problem.Fields[100]
	if summary.Path != "/" || summary.Message != "9900 more errors omitted" {
		t.Fatal(summary)
	}
	if st, e := s.State(ctx); e != nil || st.Generation != 1 {
		t.Fatal("rejection published", st, e)
	}
	if h, e := s.History(ctx); e != nil || len(h) != 1 {
		t.Fatal("rejection leaked revision", len(h), e)
	}
	wait, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	n, err := listener.WaitForNotification(wait)
	stop()
	if err == nil || n != nil {
		t.Fatal("rejection notified", n, err)
	}
	// Privileged storage bypasses compiler validation to test runtime rejection.
	p, err := s.Publish(ctx, bad, 1, postgres.Metadata{Principal: "report-test"})
	if err != nil {
		t.Fatal(err)
	}
	var statusError string
	eventually(t, 2*time.Second, func() bool {
		res, e := client.Get("http://" + boot.Admin.Listen + "/status")
		if e != nil {
			return false
		}
		defer res.Body.Close()
		var status struct {
			Desired int64  `json:"desired_generation"`
			Error   string `json:"error"`
		}
		if json.NewDecoder(res.Body).Decode(&status) != nil {
			return false
		}
		statusError = status.Error
		return res.StatusCode == 200 && status.Desired == p.Generation && strings.Contains(status.Error, summary.Message)
	})
	if len(statusError) > 8192 {
		t.Fatal("status error unbounded", len(statusError))
	}
	res, err := client.Get("http://" + boot.Server.Listen)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatal("last good snapshot lost", res.StatusCode)
	}
	stopProcess()
	lines, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range bytes.Split(bytes.TrimSpace(lines), []byte("\n")) {
		var entry struct {
			Message    string `json:"msg"`
			RevisionID int64  `json:"revision_id"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(entry.Message, "revision rejected:") {
			found = true
			if entry.RevisionID != p.RevisionID || entry.Message != statusError || len(line) > 64<<10 {
				t.Fatal("log report differs from status or is unbounded", entry.RevisionID, len(line))
			}
		}
	}
	if !found {
		t.Fatal("missing rejection log")
	}
}
