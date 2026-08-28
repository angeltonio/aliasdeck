package sqlitestore

import (
	"context"
	"fmt"
	"time"

	"github.com/angeltonio/aliasdeck/internal/store"
	"github.com/google/uuid"
)

// auditRepo implements store.AuditRepo. There is no update and no delete:
// the interface has none, and adding one here would make the table
// something other than a record.
type auditRepo struct {
	q *Queries
}

func (r auditRepo) Append(ctx context.Context, e store.AuditEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}

	if err := r.q.AppendAuditEvent(ctx, AppendAuditEventParams{
		ID:           e.ID,
		At:           formatTime(e.At),
		ActorID:      e.ActorID,
		ActorName:    e.ActorName,
		Action:       string(e.Action),
		SubjectKind:  e.SubjectKind,
		SubjectID:    e.SubjectID,
		SubjectLabel: e.SubjectLabel,
	}); err != nil {
		return fmt.Errorf("store: appending audit event: %w", err)
	}
	return nil
}

func (r auditRepo) Recent(ctx context.Context, limit int) ([]store.AuditEvent, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("store: listing audit events: invalid limit %d", limit)
	}

	rows, err := r.q.ListRecentAuditEvents(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("store: listing audit events: %w", err)
	}

	out := make([]store.AuditEvent, 0, len(rows))
	for _, row := range rows {
		at, err := parseTime(row.At)
		if err != nil {
			return nil, fmt.Errorf("store: parsing audit event timestamp: %w", err)
		}
		out = append(out, store.AuditEvent{
			ID:           row.ID,
			At:           at,
			ActorID:      row.ActorID,
			ActorName:    row.ActorName,
			Action:       store.AuditAction(row.Action),
			SubjectKind:  row.SubjectKind,
			SubjectID:    row.SubjectID,
			SubjectLabel: row.SubjectLabel,
		})
	}
	return out, nil
}

func (r auditRepo) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountAuditEvents(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: counting audit events: %w", err)
	}
	return int(n), nil
}
