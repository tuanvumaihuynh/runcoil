package postgres

import (
	"encoding/json"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/store/postgres/gen"
)

// retryPolicyJSON is how tasks.retry_policy and the retry of runs.config store a policy.
// Delays are Go duration strings, such as "1m30s".
type retryPolicyJSON struct {
	MaxAttempts int      `json:"max_attempts,omitempty"`
	Delays      []string `json:"delays,omitempty"`
}

// runConfigJSON is the part of runs.config that the engine reads: the Task's settings when
// the Run was created.
type runConfigJSON struct {
	Retry *retryPolicyJSON `json:"retry"`
}

func retryPolicyToJSON(p domain.RetryPolicy) retryPolicyJSON {
	policy := retryPolicyJSON{MaxAttempts: p.MaxAttempts}
	for _, d := range p.Delays {
		policy.Delays = append(policy.Delays, d.String())
	}

	return policy
}

func retryPolicyFromJSON(p retryPolicyJSON) (domain.RetryPolicy, error) {
	policy := domain.RetryPolicy{MaxAttempts: p.MaxAttempts}
	for _, s := range p.Delays {
		d, err := time.ParseDuration(s)
		if err != nil {
			return domain.RetryPolicy{}, fmt.Errorf("parse delay %q: %w", s, err)
		}

		policy.Delays = append(policy.Delays, d)
	}

	return policy, nil
}

// runRetryPolicy returns the retry policy in a Run's config, or nil when it has none.
func runRetryPolicy(config json.RawMessage) (*domain.RetryPolicy, error) {
	if len(config) == 0 {
		return nil, nil //nolint:nilnil // No policy is not an error.
	}

	var c runConfigJSON
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("decode run config: %w", err)
	}

	if c.Retry == nil {
		return nil, nil //nolint:nilnil // No policy is not an error.
	}

	policy, err := retryPolicyFromJSON(*c.Retry)
	if err != nil {
		return nil, fmt.Errorf("decode retry policy: %w", err)
	}

	return &policy, nil
}

func taskUpsertParams(id, projectID uuid.UUID, app string, def domain.TaskDefinition) (gen.TaskUpsertParams, error) {
	params := gen.TaskUpsertParams{
		ID:            id,
		ProjectID:     projectID,
		App:           app,
		Name:          def.Name,
		StartDeadline: interval(def.StartDeadline),
	}

	if def.Retry != nil {
		raw, err := json.Marshal(retryPolicyToJSON(*def.Retry))
		if err != nil {
			return gen.TaskUpsertParams{}, fmt.Errorf("encode retry policy: %w", err)
		}

		msg := json.RawMessage(raw)
		params.RetryPolicy = &msg
	}

	return params, nil
}

// stepRecordParams is a row of steps, whichever query read it.
type stepRecordParams struct {
	StepID       string
	Kind         string
	Status       string
	Output       []byte
	ErrorCode    *string
	ErrorMessage *string
}

// stepRecord builds a Step record from its row.
func stepRecord(params stepRecordParams) domain.StepRecord {
	record := domain.StepRecord{StepID: params.StepID, Kind: domain.StepKind(params.Kind), Output: params.Output}
	if domain.StepStatus(params.Status) != domain.StepStatusFailed {
		return record
	}

	record.Output = nil
	record.Failure = &domain.Failure{}
	if params.ErrorCode != nil {
		record.Failure.Code = domain.FailureCode(*params.ErrorCode)
	}

	if params.ErrorMessage != nil {
		record.Failure.Message = *params.ErrorMessage
	}

	return record
}

// interval converts an optional duration to an interval; nil is NULL.
func interval(d *time.Duration) pgtype.Interval {
	if d == nil {
		return pgtype.Interval{}
	}

	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

func projectRoleFromRow(row gen.ProjectMemberListByUserRow) domain.ProjectRole {
	return domain.ProjectRole{
		ProjectID:   row.ID,
		ProjectName: row.Name,
		Role:        domain.Role(row.Role),
	}
}

func workerFromRow(row gen.WorkerGetBySessionTokenRow) domain.Worker {
	return domain.Worker{
		ID:        row.ID,
		ProjectID: row.ProjectID,
		App:       row.App,
		TaskIDs:   row.TaskIds,
		Slots:     int(row.Slots),
	}
}

func assignFromRow(row gen.RunListForAssignRow, epoch int64, steps []domain.StepRecord) domain.Assign {
	return domain.Assign{
		Lease:     domain.Lease{RunID: row.ID, Epoch: epoch},
		TaskName:  row.TaskName,
		Input:     row.Input,
		Steps:     steps,
		CreatedAt: row.CreatedAt,
	}
}
