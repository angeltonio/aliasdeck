package web

import (
	"log/slog"
	"net/http"

	"github.com/angeltonio/aliasdeck/internal/store"
)

// audit records one operator action.
//
// The operator's name is looked up here rather than carried on the session,
// so it costs a read on mutations only and not on every page load — and so
// the name recorded is the one that was in use when the action happened. A
// failed lookup still records the id: knowing *which* operator acted, even
// unnamed, is most of the answer.
//
// The append never fails the request. By the time it runs the
// mutation has already succeeded and been committed, so turning an audit
// failure into a failed response would tell an operator their action did not
// happen when it did — the one lie this package must not tell. The realistic
// failure mode is a full or corrupt database, in which case the mutation
// itself would have failed first and there would be nothing to record.
//
// Dropping it silently would be the other wrong trade, though: an operator
// would have no way to learn the log has gaps. The failure is recorded
// through slog.Default() at Error level, the same treatment
// internal/api/sync.go already gives a device bookkeeping write that fails
// after the response is already owed.
func (a *webapp) audit(r *http.Request, action store.AuditAction, subjectKind, subjectID, subjectLabel string) {
	subj, ok := subjectFromContext(r.Context())
	if !ok {
		// Every caller sits behind requireSession, so this is unreachable
		// in production. Recording the action with no actor is still
		// better than recording nothing at all.
		subj = webSubject{}
	}

	name := ""
	if subj.OperatorID != "" {
		if op, err := a.store.Operators().Get(r.Context(), subj.OperatorID); err == nil {
			name = op.Username
		}
	}

	if err := a.store.Audit().Append(r.Context(), store.AuditEvent{
		At:           a.now(),
		ActorID:      subj.OperatorID,
		ActorName:    name,
		Action:       action,
		SubjectKind:  subjectKind,
		SubjectID:    subjectID,
		SubjectLabel: subjectLabel,
	}); err != nil {
		slog.Default().Error("web: failed to record an operator action",
			"action", action, "actorId", subj.OperatorID,
			"subjectKind", subjectKind, "subjectId", subjectID, "error", err)
	}
}
