package action

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrOneTimeResultNotAvailable = errors.New("one-time action result is not available")
	ErrOneTimeResultExpired      = errors.New("one-time action result expired")
	ErrOneTimeResultConsumed     = errors.New("one-time action result already consumed")
)

const oneTimeResultKindEnrollment = "connector_enrollment"

type OneTimeResult struct {
	ExecutionID uuid.UUID
	ResultKind  string
	Enrollment  resource.EnrollmentResult
	ExpiresAt   time.Time
}

type oneTimeResultPayload struct {
	Enrollment resource.EnrollmentResult `json:"enrollment"`
}

func storeOneTimeResult(ctx context.Context, q *db.Queries, key []byte, execution db.Execution, authorizationVersion int64, enrollment resource.EnrollmentResult) error {
	payload, err := json.Marshal(oneTimeResultPayload{Enrollment: enrollment})
	if err != nil {
		return err
	}
	nonce, ciphertext, err := encryptOneTimeResult(key, payload, oneTimeResultAAD(execution.EnterpriseID, execution.ID))
	clear(payload)
	if err != nil {
		return err
	}
	_, err = q.CreateExecutionOneTimeResult(ctx, db.CreateExecutionOneTimeResultParams{
		ID: newID(), ExecutionID: execution.ID, EnterpriseID: execution.EnterpriseID,
		AuthorizationVersion: authorizationVersion, ResultKind: oneTimeResultKindEnrollment,
		KeyVersion: 1, Nonce: nonce, Ciphertext: ciphertext,
		ExpiresAt: pgtype.Timestamptz{Time: enrollment.ExpiresAt.UTC(), Valid: true},
	})
	return err
}

func (service Service) ClaimOneTimeResult(ctx context.Context, actorID string, enterpriseID, executionID uuid.UUID, idempotencyKey string) (OneTimeResult, error) {
	request := struct {
		ExecutionID uuid.UUID `json:"execution_id"`
	}{ExecutionID: executionID}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID,
		"execution.one_time_result.claim", idempotencyKey, request, 200, func(q *db.Queries) (OneTimeResult, error) {
			actor, err := uuid.Parse(actorID)
			if err != nil {
				return OneTimeResult{}, ErrOneTimeResultNotAvailable
			}
			execution, err := q.GetExecution(ctx, db.GetExecutionParams{ID: executionID, EnterpriseID: enterpriseID})
			if err != nil || execution.Status != "succeeded" {
				return OneTimeResult{}, ErrOneTimeResultNotAvailable
			}
			action, err := q.GetPendingActionByID(ctx, db.GetPendingActionByIDParams{ID: execution.PendingActionID, EnterpriseID: enterpriseID})
			if err != nil || action.CreatorSubjectType != "user" || action.CreatorSubjectID != actor {
				return OneTimeResult{}, ErrOneTimeResultNotAvailable
			}
			record, err := q.GetExecutionOneTimeResultForUpdate(ctx, db.GetExecutionOneTimeResultForUpdateParams{ExecutionID: executionID, EnterpriseID: enterpriseID})
			if errors.Is(err, pgx.ErrNoRows) {
				return OneTimeResult{}, ErrOneTimeResultNotAvailable
			}
			if err != nil {
				return OneTimeResult{}, err
			}
			if record.ConsumedAt.Valid {
				return OneTimeResult{}, ErrOneTimeResultConsumed
			}
			if !time.Now().UTC().Before(record.ExpiresAt.Time) {
				return OneTimeResult{}, ErrOneTimeResultExpired
			}
			user, err := q.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: actor, EnterpriseID: enterpriseID})
			if err != nil || !oneTimeResultAuthorizationCurrent(user.Status, user.AuthorizationVersion, record.AuthorizationVersion) {
				return OneTimeResult{}, ErrInvalidated
			}
			plaintext, err := decryptOneTimeResult(service.OneTimeResultKey, record.Nonce, record.Ciphertext, oneTimeResultAAD(enterpriseID, executionID))
			if err != nil {
				return OneTimeResult{}, ErrOneTimeResultNotAvailable
			}
			defer clear(plaintext)
			var payload oneTimeResultPayload
			if err := json.Unmarshal(plaintext, &payload); err != nil || payload.Enrollment.InstallCommand == "" {
				return OneTimeResult{}, ErrOneTimeResultNotAvailable
			}
			if _, err := q.ConsumeExecutionOneTimeResult(ctx, db.ConsumeExecutionOneTimeResultParams{
				ExecutionID: executionID, EnterpriseID: enterpriseID, ConsumedByUserID: uuid.NullUUID{UUID: actor, Valid: true},
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return OneTimeResult{}, ErrOneTimeResultConsumed
				}
				return OneTimeResult{}, err
			}
			_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
				ActorType: "enterprise_user", ActorID: actorID, Action: "execution.one_time_result.claim", ResourceType: "execution",
				ResourceID: executionID.String(), Result: "success", Details: map[string]any{"result_kind": record.ResultKind}})
			if err != nil {
				return OneTimeResult{}, err
			}
			return OneTimeResult{ExecutionID: executionID, ResultKind: record.ResultKind, Enrollment: payload.Enrollment, ExpiresAt: record.ExpiresAt.Time}, nil
		})
}

func oneTimeResultAuthorizationCurrent(userStatus string, currentVersion, resultVersion int64) bool {
	return userStatus == "active" && currentVersion == resultVersion
}

func encryptOneTimeResult(key, plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := oneTimeResultAEAD(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func decryptOneTimeResult(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := oneTimeResultAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, ErrOneTimeResultNotAvailable
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func oneTimeResultAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("one-time result encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func oneTimeResultAAD(enterpriseID, executionID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("argus.action_one_time_result/v1\x00%s\x00%s", enterpriseID, executionID))
}
