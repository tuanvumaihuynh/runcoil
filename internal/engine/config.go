package engine

import (
	"errors"
	"time"

	"github.com/tuanvumaihuynh/runcoil/internal/domain"
)

// Config is the configuration of the engine.
type Config struct {
	Port int `yaml:"port"`
	// Token is the bearer token every request must present.
	//nolint:gosec // The token is configuration, not a hardcoded secret.
	Token string `yaml:"token"`
	// HeartbeatInterval is how often the server sends keepalive on a stream.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	// LeaseDuration is how long an attempt stays leased without a heartbeat.
	LeaseDuration time.Duration `yaml:"lease_duration"`
	// SendTimeout bounds how long a dispatch may wait to be written to a stream.
	SendTimeout time.Duration `yaml:"send_timeout"`
	// Retry is the default retry policy, used when neither the Step nor the Task sets one.
	Retry RetryConfig `yaml:"retry"`
	// WorkerWaitLimit ends a Run that has waited for a worker for longer than this.
	WorkerWaitLimit time.Duration `yaml:"worker_wait_limit"`
	// InstanceEvictionLeases is how many lease durations an instance may stay silent
	// before the registry drops it, so the registry forgets an instance that has gone away.
	// The default is 3.
	InstanceEvictionLeases int `yaml:"instance_eviction_leases"`
	// Limits are the size limits on user data.
	Limits LimitsConfig `yaml:"limits"`
	// Scheduler is the schedule poller.
	Scheduler SchedulerConfig `yaml:"scheduler"`
	// Dispatcher is the dispatch loop.
	Dispatcher DispatcherConfig `yaml:"dispatcher"`
	// Sweep is the pass that expires leases, worker waits, and instances.
	Sweep SweepConfig `yaml:"sweep"`
	// Idempotency is how long a caller's key is held.
	Idempotency IdempotencyConfig `yaml:"idempotency"`
	// WaitRun bounds how long a WaitRun call may wait.
	WaitRun WaitRunConfig `yaml:"wait_run"`
}

// IdempotencyConfig is the window of a caller's idempotency key. A repeat within the window
// returns the first Run. The default is 24 hours.
type IdempotencyConfig struct {
	Window time.Duration `yaml:"window"`
}

// WaitRunConfig bounds WaitRun. A request without a timeout waits MaxTimeout. The
// poll interval is how often a waiting call checks the store, besides the signal.
type WaitRunConfig struct {
	MaxTimeout   time.Duration `yaml:"max_timeout"`
	PollInterval time.Duration `yaml:"poll_interval"`
	// MaxWaiters is how many WaitRun calls may wait at once. A call over it is refused.
	MaxWaiters int `yaml:"max_waiters"`
}

// SchedulerConfig is the schedule poller. Only the schedules due within Window are
// read into memory, and at most MaxLoaded of them. A poll runs every PollInterval.
type SchedulerConfig struct {
	Window       time.Duration `yaml:"window"`
	PollInterval time.Duration `yaml:"poll_interval"`
	MaxLoaded    int           `yaml:"max_loaded"`
}

// DispatcherConfig is the dispatch loop's poll. It also runs when signalled.
type DispatcherConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
}

// SweepConfig is the interval of the pass that expires leases and worker waits, and
// enforces instance liveness.
type SweepConfig struct {
	Interval time.Duration `yaml:"interval"`
}

// LimitsConfig is the size limits, in bytes.
type LimitsConfig struct {
	InputBytes      int `yaml:"input_bytes"`
	StepOutputBytes int `yaml:"step_output_bytes"`
	RunOutputBytes  int `yaml:"run_output_bytes"`
	RunTotalBytes   int `yaml:"run_total_bytes"`
}

func (c LimitsConfig) domain() domain.Limits {
	return domain.Limits{
		Input:      c.InputBytes,
		StepOutput: c.StepOutputBytes,
		RunOutput:  c.RunOutputBytes,
		RunTotal:   c.RunTotalBytes,
	}
}

// RetryConfig is the server's default retry policy.
type RetryConfig struct {
	// MaxAttempts is the number of tries in total, the first one included.
	MaxAttempts int `yaml:"max_attempts"`
	// Delays is the delay before each retry. The last one repeats.
	Delays []time.Duration `yaml:"delays"`
	// Jitter varies each table delay by up to this fraction either way. It is from 0 to below 1.
	Jitter float64 `yaml:"jitter"`
}

func (c RetryConfig) domain() domain.RetryConfig {
	return domain.RetryConfig{MaxAttempts: c.MaxAttempts, Delays: c.Delays, Jitter: c.Jitter}
}

// readOverheadBytes is the room a request has beyond its largest payload: the
// envelope, the metadata, and the Steps of a Dispatch that are not payload.
const readOverheadBytes = 1 << 20

// ReadMaxBytes is the most a request body may hold. It is the largest single payload
// a request can carry, once base64-encoded as JSON, plus a fixed overhead. The run
// total is not a single payload, so it does not set the cap.
func (c Config) ReadMaxBytes() int {
	largest := max(c.Limits.InputBytes, c.Limits.StepOutputBytes, c.Limits.RunOutputBytes)
	encoded := (largest + 2) / 3 * 4
	return encoded + readOverheadBytes
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be from 1 to 65535")
	}

	if c.Token == "" {
		return errors.New("token is required: set ENGINE__TOKEN")
	}

	if c.HeartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be greater than 0")
	}

	if c.LeaseDuration <= c.HeartbeatInterval {
		return errors.New("lease duration must be greater than the heartbeat interval")
	}

	if c.SendTimeout <= 0 {
		return errors.New("send timeout must be greater than 0")
	}

	if c.WorkerWaitLimit <= 0 {
		return errors.New("worker wait limit must be greater than 0")
	}

	if c.InstanceEvictionLeases < 1 {
		return errors.New("instance eviction must be at least 1 lease duration")
	}

	if err := c.Limits.Validate(); err != nil {
		return err
	}

	if err := c.Scheduler.Validate(); err != nil {
		return err
	}

	if c.Dispatcher.PollInterval <= 0 {
		return errors.New("dispatcher poll interval must be greater than 0")
	}

	if c.Sweep.Interval <= 0 {
		return errors.New("sweep interval must be greater than 0")
	}

	if c.Idempotency.Window <= 0 {
		return errors.New("idempotency window must be greater than 0")
	}

	if err := c.validateWaitRun(); err != nil {
		return err
	}

	return c.Retry.Validate()
}

// validateWaitRun checks the WaitRun bounds.
func (c *Config) validateWaitRun() error {
	if c.WaitRun.MaxTimeout <= 0 {
		return errors.New("wait run max timeout must be greater than 0")
	}

	if c.WaitRun.PollInterval <= 0 {
		return errors.New("wait run poll interval must be greater than 0")
	}

	if c.WaitRun.MaxWaiters < 1 {
		return errors.New("wait run max waiters must be at least 1")
	}

	return nil
}

// Validate checks the scheduler's window, poll interval, and cap. The poll interval
// may not exceed the window, so that every tick due within the window is loaded in time.
func (c SchedulerConfig) Validate() error {
	if c.Window <= 0 {
		return errors.New("scheduler window must be greater than 0")
	}

	if c.PollInterval <= 0 || c.PollInterval > c.Window {
		return errors.New("scheduler poll interval must be greater than 0 and at most the window")
	}

	if c.MaxLoaded < 1 {
		return errors.New("scheduler max loaded must be at least 1")
	}

	return nil
}

// Validate checks the size limits.
func (c LimitsConfig) Validate() error {
	if c.InputBytes <= 0 || c.StepOutputBytes <= 0 || c.RunOutputBytes <= 0 || c.RunTotalBytes <= 0 {
		return errors.New("size limits must be greater than 0")
	}

	if c.RunTotalBytes < c.InputBytes {
		return errors.New("run total limit must not be below the input limit")
	}

	return nil
}

// Validate checks the retry configuration.
func (c RetryConfig) Validate() error {
	if c.MaxAttempts < 1 {
		return errors.New("retry max attempts must be at least 1")
	}

	if len(c.Delays) == 0 {
		return errors.New("retry delays are required")
	}

	for _, d := range c.Delays {
		if d <= 0 {
			return errors.New("retry delays must be greater than 0")
		}
	}

	if c.Jitter < 0 || c.Jitter >= 1 {
		return errors.New("retry jitter must be from 0 up to, not including, 1")
	}

	return nil
}
