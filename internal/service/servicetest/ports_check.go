package servicetest

import "technical-test-be-gdc-multi-sarana/internal/service"

// Compile-time checks that the fakes satisfy the service ports.
var (
	_ service.TxManager             = (*TxManager)(nil)
	_ service.TeamRepository        = (*TeamRepository)(nil)
	_ service.UserRepository        = (*UserRepository)(nil)
	_ service.TaskRepository        = (*TaskRepository)(nil)
	_ service.TaskLogRepository     = (*TaskLogRepository)(nil)
	_ service.IdempotencyRepository = (*IdempotencyRepository)(nil)
	_ service.PasswordHasher        = (*Hasher)(nil)
	_ service.TokenIssuer           = (*TokenIssuer)(nil)
	_ service.Notifier              = (*Notifier)(nil)
	_ service.KeyLocker             = NoLock{}
)
