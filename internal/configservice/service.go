// Package configservice is the one validation/publication path used by management.
package configservice

import (
	"context"
	"fmt"

	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/gateway"
	"github.com/kmpoltorak/routetus/internal/postgres"
)

type Service struct {
	Store    *postgres.Store
	Compiler *gateway.Compiler
}

func (s *Service) Validate(c config.Config) error {
	v, err := s.Compiler.Compile(c)
	if err != nil {
		return err
	}
	v.Close()
	return nil
}
func (s *Service) Publish(ctx context.Context, c config.Config, generation int64, m postgres.Metadata) (postgres.Publication, error) {
	if len(m.Author) > 256 || len(m.Description) > 4096 {
		return postgres.Publication{}, config.Invalid("/metadata", "author or description too long")
	}
	if err := s.Validate(c); err != nil {
		return postgres.Publication{}, err
	}
	if m.Principal == "" {
		return postgres.Publication{}, fmt.Errorf("authenticated principal required")
	}
	return s.Store.Publish(ctx, c, generation, m)
}
func (s *Service) Rollback(ctx context.Context, id, generation int64, m postgres.Metadata) (postgres.Publication, error) {
	c, err := s.Store.Revision(ctx, id)
	if err != nil {
		return postgres.Publication{}, err
	}
	return s.Publish(ctx, c, generation, m)
}
