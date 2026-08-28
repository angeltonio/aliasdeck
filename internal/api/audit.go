package api

import (
	"log/slog"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/auth"
	"github.com/angeltonio/aliasdeck/internal/store"
)

// audit records one operator action performed through the REST API.
//
// The API is audited for the same reason the browser is: an audit log with a
// known blind spot is worse than none, because it is trusted for the thing it
// missed. Every route this is called from requires a session token, so the
// actor is always an operator rather than a device.
//
// Device sync and heartbeat are deliberately absent. They are device traffic,
// not operator actions, and they run every few seconds per machine — the
// handful of rows worth reading would be unfindable underneath them.
//
// This mirrors internal/web's helper exactly, including the trade: the append
// never fails the request, because by the time it runs the mutation has
// committed and reporting a failure would say the action did not happen when
// it did. A failed append is logged rather than dropped, the same treatment
// handleSync already gives a device bookkeeping write that fails after the
// response is owed.
func (a *api) audit(r *http.Request, action store.AuditAction, subjectKind, subjectID, subjectLabel string) {
	actorID := ""
	if subj, ok := auth.SubjectFromContext(r.Context()); ok {
		actorID = subj.SubjectID
	}

	name := ""
	if actorID != "" {
		if op, err := a.store.Operators().Get(r.Context(), actorID); err == nil {
			name = op.Username
		}
	}

	if err := a.store.Audit().Append(r.Context(), store.AuditEvent{
		At:           a.now(),
		ActorID:      actorID,
		ActorName:    name,
		Action:       action,
		SubjectKind:  subjectKind,
		SubjectID:    subjectID,
		SubjectLabel: subjectLabel,
	}); err != nil {
		slog.Default().Error("api: failed to record an operator action",
			"action", action, "actorId", actorID,
			"subjectKind", subjectKind, "subjectId", subjectID, "error", err)
	}
}
