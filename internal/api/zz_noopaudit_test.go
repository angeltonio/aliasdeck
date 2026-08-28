package api

import (
	"context"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// noopAuditRepo lets a fake store satisfy store.Store without recording
// anything. It is a type rather than a nil return so that code under test
// which starts auditing fails an assertion instead of panicking on a nil
// interface — a dropped audit record should be visible, not fatal.
type noopAuditRepo struct{}

func (noopAuditRepo) Append(context.Context, store.AuditEvent) error { return nil }

func (noopAuditRepo) Recent(context.Context, int) ([]store.AuditEvent, error) { return nil, nil }

func (noopAuditRepo) Count(context.Context) (int, error) { return 0, nil }
