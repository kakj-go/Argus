package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key request mismatch or operation in progress")
	ErrIdempotencyExpired  = errors.New("idempotency result expired")
)

type Idempotency struct{ Key []byte }

type IdempotencyResult struct {
	Replay bool
	Status int
	Body   []byte
}

func ExecuteIdempotent[T any](ctx context.Context, store *Store, service Idempotency, audience, subjectID, operation, key string, request any, status int, fn func(*db.Queries) (T, error)) (T, error) {
	var result T
	requestBody, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	err = store.InTx(ctx, func(queries *db.Queries) error {
		begin, err := service.Begin(ctx, queries, audience, subjectID, operation, key, requestBody)
		if err != nil {
			return err
		}
		if begin.Replay {
			return json.Unmarshal(begin.Body, &result)
		}
		result, err = fn(queries)
		if err != nil {
			return err
		}
		responseBody, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return service.Complete(ctx, queries, audience, subjectID, operation, key, status, responseBody)
	})
	return result, err
}

func (service Idempotency) Begin(ctx context.Context, queries *db.Queries, audience, subjectID, operation, key string, request []byte) (IdempotencyResult, error) {
	hash := sha256.Sum256(request)
	rows, err := queries.CreateIdempotencyRecord(ctx, db.CreateIdempotencyRecordParams{
		Audience: audience, SubjectID: subjectID, Operation: operation, IdempotencyKey: key, RequestHash: hash[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(5 * time.Minute), Valid: true},
	})
	if err != nil {
		return IdempotencyResult{}, err
	}
	if rows == 1 {
		return IdempotencyResult{}, nil
	}
	record, err := queries.GetIdempotencyRecord(ctx, db.GetIdempotencyRecordParams{Audience: audience, SubjectID: subjectID, Operation: operation, IdempotencyKey: key})
	if err != nil {
		return IdempotencyResult{}, err
	}
	if subtle.ConstantTimeCompare(record.RequestHash, hash[:]) != 1 {
		return IdempotencyResult{}, ErrIdempotencyConflict
	}
	if !time.Now().UTC().Before(record.ExpiresAt.Time) {
		return IdempotencyResult{}, ErrIdempotencyExpired
	}
	if !record.ResponseStatus.Valid || len(record.ResponseCiphertext) == 0 {
		return IdempotencyResult{}, ErrIdempotencyConflict
	}
	body, err := service.decrypt(record.ResponseNonce, record.ResponseCiphertext, []byte(audience+"\x00"+subjectID+"\x00"+operation+"\x00"+key))
	if err != nil {
		return IdempotencyResult{}, err
	}
	return IdempotencyResult{Replay: true, Status: int(record.ResponseStatus.Int32), Body: body}, nil
}

func (service Idempotency) Complete(ctx context.Context, queries *db.Queries, audience, subjectID, operation, key string, status int, body []byte) error {
	aad := []byte(audience + "\x00" + subjectID + "\x00" + operation + "\x00" + key)
	nonce, ciphertext, err := service.encrypt(body, aad)
	if err != nil {
		return err
	}
	rows, err := queries.CompleteIdempotencyRecord(ctx, db.CompleteIdempotencyRecordParams{Audience: audience, SubjectID: subjectID, Operation: operation, IdempotencyKey: key,
		ResponseStatus: pgtype.Int4{Int32: int32(status), Valid: true}, ResponseNonce: nonce, ResponseCiphertext: ciphertext})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrIdempotencyExpired
	}
	return nil
}

func (service Idempotency) encrypt(plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := service.aead()
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}
func (service Idempotency) decrypt(nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := service.aead()
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}
func (service Idempotency) aead() (cipher.AEAD, error) {
	if len(service.Key) != 32 {
		return nil, errors.New("idempotency encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(service.Key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
