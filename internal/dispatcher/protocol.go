package dispatcher

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"time"

	"github.com/runcoil/runcoil/internal/apperr"
	"github.com/runcoil/runcoil/internal/domain"
)

// sessionTokenBytes is the size of a session token: 256 random bits.
const sessionTokenBytes = 32

var (
	errInvalidAPIKey       = apperr.Unauthenticated(apperr.CodeUnauthenticated, "invalid api key")
	errInvalidSessionToken = apperr.Unauthenticated(apperr.CodeUnauthenticated, "invalid session token")
	errStaleLease          = apperr.FailedPrecondition(apperr.CodeStaleLease, "lease is no longer this attempt's")
)

// HeartbeatInterval is how often a worker sends a heartbeat.
func (s *Service) HeartbeatInterval() time.Duration {
	return s.cfg.HeartbeatInterval
}

// RegisterResult is the session Register opens. The worker sends SessionToken on every
// other call.
type RegisterResult struct {
	Worker       domain.Worker
	SessionToken string
}

// Register stores the Tasks a worker process declares and opens its session.
func (s *Service) Register(ctx context.Context, apiKey string, reg domain.Registration) (RegisterResult, error) {
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return RegisterResult{}, fmt.Errorf("new session token: %w", err)
	}

	session, err := domain.NewWorkerSession(reg, tokenHash)
	if regErr, ok := domain.AsRegistrationError(err); ok {
		return RegisterResult{}, registrationError(regErr)
	}

	if err != nil {
		return RegisterResult{}, fmt.Errorf("new worker session: %w", err)
	}

	keyHash := sha256.Sum256([]byte(apiKey))
	worker, err := s.workerStore.Register(ctx, keyHash[:], session)
	if errors.Is(err, domain.ErrAPIKeyNotFound) {
		return RegisterResult{}, errInvalidAPIKey
	}

	if err != nil {
		return RegisterResult{}, fmt.Errorf("register worker: %w", err)
	}

	tasks := make([]string, 0, len(reg.Tasks))
	for _, t := range reg.Tasks {
		tasks = append(tasks, t.Name)
	}

	s.logger.InfoContext(ctx, "worker registered",
		slog.String("worker_id", worker.ID.String()),
		slog.String("app", worker.App),
		slog.Int("slots", worker.Slots),
		slog.Any("tasks", tasks),
	)

	return RegisterResult{Worker: worker, SessionToken: token}, nil
}

// outputTooLarge refuses the output of a Step or a Run over the limit. The worker reports
// the failure in its place.
func outputTooLarge(of string, size int) error {
	return apperr.Invalid(apperr.CodeOutputTooLarge,
		fmt.Sprintf("%s output is %d bytes, over the limit of %d bytes", of, size, domain.MaxOutputBytes))
}

func registrationError(regErr *domain.RegistrationError) error {
	appErr := apperr.Invalid(apperr.CodeInvalidRegistration, regErr.Message)
	if regErr.TaskName != "" {
		appErr = appErr.WithDetail("task_name", regErr.TaskName)
	}

	return appErr
}

func newSessionToken() (string, []byte, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("read random bytes: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	return token, hash[:], nil
}

// Authenticate returns the live session of the token.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Worker, error) {
	hash := sha256.Sum256([]byte(token))
	worker, err := s.workerStore.GetWorkerBySessionToken(ctx, hash[:])
	if errors.Is(err, domain.ErrSessionEnded) {
		return domain.Worker{}, errInvalidSessionToken
	}

	if err != nil {
		return domain.Worker{}, fmt.Errorf("get worker by session token: %w", err)
	}

	return worker, nil
}

// Heartbeat records that the worker process is alive. The next flush writes it.
func (s *Service) Heartbeat(worker domain.Worker) {
	s.beatsMu.Lock()
	defer s.beatsMu.Unlock()

	s.beats[worker.ID] = time.Now()
}

// Deregister ends the worker's session at the end of its drain. The attempts it still
// holds go back to the queue without counting as lost, and its stream on this server
// ends.
func (s *Service) Deregister(ctx context.Context, worker domain.Worker, reason string) error {
	released, err := s.workerStore.Deregister(ctx, worker.ID)
	if errors.Is(err, domain.ErrSessionEnded) {
		return errInvalidSessionToken
	}

	if err != nil {
		return fmt.Errorf("deregister: %w", err)
	}

	s.endWorker(worker.ID)

	s.logger.InfoContext(ctx, "worker deregistered",
		slog.String("worker_id", worker.ID.String()),
		slog.String("app", worker.App),
		slog.String("reason", reason),
		slog.Int("released", released),
	)

	return nil
}

// CompleteStep stores the outcome of one try of a Do Step and returns its record. A
// failed try that may be tried again ends the attempt instead: the result carries the
// retry, the Run waits for its next try, and the worker's slot is free.
func (s *Service) CompleteStep(ctx context.Context, params domain.CompleteStepParams) (domain.StepResult, error) {
	if err := params.Validate(); err != nil {
		return domain.StepResult{}, apperr.Invalid(apperr.CodeInvalidRequest, err.Error())
	}

	if n := len(params.Outcome.Output); n > domain.MaxOutputBytes {
		return domain.StepResult{}, outputTooLarge("step", n)
	}

	result, err := s.runStore.CompleteStep(ctx, params, s.retryDecider(params.Outcome, params.Retry))
	if errors.Is(err, domain.ErrStaleLease) {
		s.release(params.WorkerID, params.Lease)
		return domain.StepResult{}, errStaleLease
	}

	if err != nil {
		return domain.StepResult{}, fmt.Errorf("complete step: %w", err)
	}

	if result.Retry != nil {
		s.release(params.WorkerID, params.Lease)

		s.logger.DebugContext(ctx, "step retry scheduled",
			slog.String("run_id", params.Lease.RunID.String()),
			slog.String("step_id", params.StepID),
			slog.Int("try_count", result.Retry.TryCount),
			slog.Time("next_attempt_at", result.Retry.NextAttemptAt),
		)

		return result, nil
	}

	status := domain.StepStatusSucceeded
	if result.Record.Failure != nil {
		status = domain.StepStatusFailed
	}

	s.logger.DebugContext(ctx, "step recorded",
		slog.String("run_id", params.Lease.RunID.String()),
		slog.String("step_id", result.Record.StepID),
		slog.String("status", string(status)),
	)

	return result, nil
}

// retryDecider decides the next try of a failed Step. The Step's own policy comes first,
// then the Run's, then the server's default, field by field. A retry_after from the worker
// replaces the policy's delay; any other delay is spread by the jitter.
func (s *Service) retryDecider(outcome domain.StepOutcome, retry domain.StepRetry) domain.RetryDecider {
	return func(tries int, runPolicy *domain.RetryPolicy) (time.Duration, bool) {
		if outcome.Failure == nil || !outcome.Failure.Code.Retriable() || retry.NonRetriable {
			return 0, false
		}

		policy := s.cfg.Retry.Policy()
		if runPolicy != nil {
			policy = runPolicy.Or(policy)
		}

		if retry.Policy != nil {
			policy = retry.Policy.Or(policy)
		}

		delay, ok := policy.Delay(tries)
		if !ok {
			return 0, false
		}

		if retry.After != nil {
			return *retry.After, true
		}

		return jitter(delay, s.cfg.Retry.Jitter), true
	}
}

// jitter spreads d by up to fraction of itself, either way.
func jitter(d time.Duration, fraction float64) time.Duration {
	if d <= 0 || fraction <= 0 {
		return d
	}

	spread := float64(d) * fraction * (2*mathrand.Float64() - 1) //nolint:gosec // Jitter needs no secure random.

	return d + time.Duration(spread)
}

// CompleteRun ends the attempt with the handler's result. Unless the store fails, the
// worker no longer holds the attempt afterwards, so its slot is free.
func (s *Service) CompleteRun(ctx context.Context, worker domain.Worker, lease domain.Lease, result domain.RunResult) error {
	if n := len(result.Output); n > domain.MaxOutputBytes {
		return outputTooLarge("run", n)
	}

	end := result.End()
	ended, err := s.runStore.FinishRun(ctx, worker.ID, lease, end)
	if errors.Is(err, domain.ErrStaleLease) {
		s.release(worker.ID, lease)
		return errStaleLease
	}

	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	s.release(worker.ID, lease)

	if !ended {
		s.logger.DebugContext(ctx, "run already ended", slog.String("run_id", lease.RunID.String()))
		return nil
	}

	attrs := []slog.Attr{
		slog.String("run_id", lease.RunID.String()),
		slog.Int64("epoch", lease.Epoch),
		slog.String("worker_id", worker.ID.String()),
		slog.String("status", string(end.Status)),
	}
	if end.Failure != nil {
		attrs = append(attrs, slog.String("error_code", string(end.Failure.Code)))
	}

	s.logger.LogAttrs(ctx, slog.LevelDebug, "run ended", attrs...)

	return nil
}
