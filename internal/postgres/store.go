// Package postgres owns the configuration database and transactional publication.
package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kmpoltorak/routeqtus/internal/bootstrap"
	"github.com/kmpoltorak/routeqtus/internal/config"
)

//go:embed migrations/001.sql
var migration string
var ErrPrecondition = errors.New("generation precondition failed")
var ErrNotFound = errors.New("revision not found")

type Store struct {
	Pool    *pgxpool.Pool
	Timeout time.Duration
	connect *pgx.ConnConfig
}
type State struct {
	RevisionID int64 `json:"revision_id"`
	Generation int64 `json:"generation"`
}
type Revision struct {
	ID            int64     `json:"revision_id"`
	Generation    int64     `json:"generation"`
	SchemaVersion int       `json:"schema_version"`
	Principal     string    `json:"principal"`
	Author        string    `json:"author"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
}
type Metadata struct {
	Principal   string
	Author      string
	Description string
}
type Publication struct {
	State
	Status string `json:"status"`
}

func Open(ctx context.Context, c bootstrap.Postgres) (*Store, error) {
	dsn := os.Getenv(c.DSNEnv)
	if dsn == "" {
		return nil, fmt.Errorf("postgres DSN environment reference is unavailable")
	}
	p, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres DSN")
	}
	p.MaxConns = c.MaxConns
	p.MinConns = c.MinConns
	p.MaxConnLifetime = bootstrap.Duration(c.MaxConnLifetime)
	p.ConnConfig.ConnectTimeout = bootstrap.Duration(c.ConnectTimeout)
	p.ConnConfig.RuntimeParams["search_path"] = "routeqtus,public"
	pool, err := pgxpool.NewWithConfig(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize postgres pool")
	}
	s := &Store{Pool: pool, Timeout: bootstrap.Duration(c.QueryTimeout), connect: p.ConnConfig.Copy()}
	if err = s.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres connection unavailable")
	}
	return s, nil
}
func (s *Store) deadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.Timeout)
}
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	return s.Pool.Ping(ctx)
}
func (s *Store) Close() { s.Pool.Close() }
func (s *Store) CheckSchema(ctx context.Context) error {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	var v int
	err := s.Pool.QueryRow(ctx, "SELECT max(version) FROM routeqtus.schema_migrations").Scan(&v)
	if err != nil || v != 1 {
		return fmt.Errorf("unsupported or missing database schema; run migrate")
	}
	return nil
}
func (s *Store) Migrate(ctx context.Context) error {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(723001881)"); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT to_regclass('routeqtus.schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return err
	}
	if exists {
		var v int
		if err = tx.QueryRow(ctx, "SELECT max(version) FROM routeqtus.schema_migrations").Scan(&v); err != nil {
			return err
		}
		if v != 1 {
			return fmt.Errorf("unsupported schema version")
		}
	} else {
		if _, err = tx.Exec(ctx, migration); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) State(ctx context.Context) (State, error) {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	return readState(ctx, s.Pool)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readState(ctx context.Context, q rowQuerier) (State, error) {
	var st State
	err := q.QueryRow(ctx, "SELECT coalesce(active_revision,0),generation FROM gateway_state WHERE singleton").Scan(&st.RevisionID, &st.Generation)
	return st, err
}
func (s *Store) Load(ctx context.Context) (State, config.Config, error) {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return State{}, config.Config{}, err
	}
	defer rollback(tx)
	st, err := readState(ctx, tx)
	if err != nil {
		return st, config.Config{}, err
	}
	if st.RevisionID == 0 {
		return st, Empty(), tx.Commit(ctx)
	}
	c, err := loadRevision(ctx, tx, st.RevisionID)
	if err != nil {
		return st, c, err
	}
	return st, c, tx.Commit(ctx)
}
func Empty() config.Config {
	return config.Config{SchemaVersion: 1, Routes: []config.Route{}, Upstreams: []config.Upstream{}, Handlers: []config.Handler{}, GlobalHandlers: []string{}}
}
func (s *Store) Revision(ctx context.Context, id int64) (config.Config, error) {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	return loadRevision(ctx, s.Pool, id)
}

type querier interface {
	rowQuerier
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadRevision(ctx context.Context, q querier, id int64) (config.Config, error) {
	c := Empty()
	err := q.QueryRow(ctx, "SELECT schema_version,trusted_proxies,metrics FROM config_revisions WHERE id=$1 AND sealed", id).Scan(&c.SchemaVersion, &c.TrustedProxies, &c.Metrics)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	rows, err := q.Query(ctx, `SELECT jsonb_build_object('id',id,'dial_timeout',dial_timeout,'tls_handshake_timeout',tls_handshake_timeout,'response_header_timeout',response_header_timeout,'idle_timeout',idle_timeout,'max_conns',max_conns,'max_idle_conns',max_idle_conns,'max_idle_conns_per_host',max_idle_conns_per_host,'proxy_from_environment',proxy_from_environment,'tls',jsonb_build_object('server_name',tls_server_name,'ca_env',tls_ca_env,'cert_env',tls_cert_env,'key_env',tls_key_env)) FROM upstreams WHERE revision_id=$1 ORDER BY id`, id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var raw []byte
		var u config.Upstream
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return c, err
		}
		if err = json.Unmarshal(raw, &u); err != nil {
			rows.Close()
			return c, err
		}
		u.Targets = []config.Target{}
		c.Upstreams = append(c.Upstreams, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	rows, err = q.Query(ctx, "SELECT upstream_id,id,url FROM upstream_targets WHERE revision_id=$1 ORDER BY upstream_id,position", id)
	if err != nil {
		return c, err
	}
	index := map[string]int{}
	for i, u := range c.Upstreams {
		index[u.ID] = i
	}
	for rows.Next() {
		var uid string
		var t config.Target
		if err = rows.Scan(&uid, &t.ID, &t.URL); err != nil {
			rows.Close()
			return c, err
		}
		i := index[uid]
		c.Upstreams[i].Targets = append(c.Upstreams[i].Targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	rows, err = q.Query(ctx, "SELECT id,type,version,enabled,options FROM handlers WHERE revision_id=$1 ORDER BY id", id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var h config.Handler
		if err = rows.Scan(&h.ID, &h.Type, &h.Version, &h.Enabled, &h.Options); err != nil {
			rows.Close()
			return c, err
		}
		c.Handlers = append(c.Handlers, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	rows, err = q.Query(ctx, `SELECT jsonb_build_object('id',id,'host',host,'methods',methods,'path',path,'prefix',prefix,'priority',priority,'strip_prefix',strip_prefix,'host_header',host_header,'upstream',upstream_id,'request_timeout',request_timeout,'metrics',metrics,'handlers','[]'::jsonb) FROM routes WHERE revision_id=$1 ORDER BY id`, id)
	if err != nil {
		return c, err
	}
	index = map[string]int{}
	for rows.Next() {
		var raw []byte
		var r config.Route
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return c, err
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			rows.Close()
			return c, err
		}
		index[r.ID] = len(c.Routes)
		c.Routes = append(c.Routes, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	rows, err = q.Query(ctx, "SELECT route_id,handler_id FROM handler_bindings WHERE revision_id=$1 ORDER BY route_id NULLS FIRST,position", id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var rid *string
		var hid string
		if err = rows.Scan(&rid, &hid); err != nil {
			rows.Close()
			return c, err
		}
		if rid == nil {
			c.GlobalHandlers = append(c.GlobalHandlers, hid)
		} else {
			i := index[*rid]
			c.Routes[i].Handlers = append(c.Routes[i].Handlers, hid)
		}
	}
	err = rows.Err()
	rows.Close()
	return c, err
}
func duration(d *config.Duration) any {
	if d == nil {
		return nil
	}
	return d.String()
}
func nonnil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
func (s *Store) Publish(ctx context.Context, c config.Config, expected int64, m Metadata) (Publication, error) {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer rollback(tx)
	var generation int64
	if err = tx.QueryRow(ctx, "SELECT generation FROM gateway_state WHERE singleton FOR UPDATE").Scan(&generation); err != nil {
		return Publication{}, err
	}
	if generation != expected {
		return Publication{}, ErrPrecondition
	}
	var id int64
	err = tx.QueryRow(ctx, "INSERT INTO config_revisions(schema_version,trusted_proxies,metrics,principal,author,description,generation) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id", c.SchemaVersion, nonnil(c.TrustedProxies), c.Metrics, m.Principal, m.Author, m.Description, generation+1).Scan(&id)
	if err != nil {
		return Publication{}, err
	}
	batch := &pgx.Batch{}
	for _, u := range c.Upstreams {
		batch.Queue(`INSERT INTO upstreams VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, id, u.ID, duration(u.DialTimeout), duration(u.TLSHandshakeTimeout), duration(u.ResponseHeaderTimeout), duration(u.IdleTimeout), u.MaxConns, u.MaxIdleConns, u.MaxIdleConnsPerHost, u.ProxyFromEnvironment, u.TLS.ServerName, u.TLS.CAEnv, u.TLS.CertEnv, u.TLS.KeyEnv)
		for i, t := range u.Targets {
			batch.Queue("INSERT INTO upstream_targets VALUES($1,$2,$3,$4,$5)", id, u.ID, t.ID, t.URL, i)
		}
	}
	for _, h := range c.Handlers {
		batch.Queue("INSERT INTO handlers VALUES($1,$2,$3,$4,$5,$6)", id, h.ID, h.Type, h.Version, h.Enabled, []byte(h.Options))
	}
	for _, r := range c.Routes {
		batch.Queue("INSERT INTO routes VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", id, r.ID, r.Host, nonnil(r.Methods), r.Path, r.Prefix, r.Priority, r.StripPrefix, r.HostHeader, r.Upstream, duration(r.RequestTimeout), r.Metrics)
		for i, h := range r.Handlers {
			batch.Queue("INSERT INTO handler_bindings VALUES($1,$2,$3,$4)", id, r.ID, i, h)
		}
	}
	for i, h := range c.GlobalHandlers {
		batch.Queue("INSERT INTO handler_bindings VALUES($1,NULL,$2,$3)", id, i, h)
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return Publication{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE config_revisions SET sealed=true WHERE id=$1", id); err != nil {
		return Publication{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE gateway_state SET active_revision=$1,generation=generation+1 WHERE singleton", id); err != nil {
		return Publication{}, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_notify('routeqtus_config',$1)", strconv.FormatInt(generation+1, 10)); err != nil {
		return Publication{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Publication{}, err
	}
	return Publication{State: State{id, generation + 1}, Status: "committed"}, nil
}
func (s *Store) History(ctx context.Context) ([]Revision, error) {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	rows, err := s.Pool.Query(ctx, "SELECT id,generation,schema_version,principal,author,description,created_at FROM config_revisions WHERE sealed ORDER BY generation DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err = rows.Scan(&r.ID, &r.Generation, &r.SchemaVersion, &r.Principal, &r.Author, &r.Description, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Listen(ctx context.Context) (*pgx.Conn, error) {
	ctx, cancel := s.deadline(ctx)
	defer cancel()
	c, err := pgx.ConnectConfig(ctx, s.connect.Copy())
	if err != nil {
		return nil, err
	}
	if _, err = c.Exec(ctx, "LISTEN routeqtus_config"); err != nil {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.Close(closeCtx)
		closeCancel()
		return nil, err
	}
	return c, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
