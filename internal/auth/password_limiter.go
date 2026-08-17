package auth

// PasswordVerificationConcurrency is the process-wide upper bound for
// concurrent Argon2 password checks across both the JSON API and web UI.
const PasswordVerificationConcurrency = 4

// PasswordLimiter is shared by every password-login transport in one server
// process. TryAcquire deliberately fails fast: unauthenticated excess work is
// rejected instead of being allowed to build an unbounded queue.
type PasswordLimiter chan struct{}

func NewPasswordLimiter() PasswordLimiter {
	return make(PasswordLimiter, PasswordVerificationConcurrency)
}

func (l PasswordLimiter) TryAcquire() bool {
	select {
	case l <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l PasswordLimiter) Release() { <-l }
