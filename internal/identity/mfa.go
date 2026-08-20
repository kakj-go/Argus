package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // TOTP interoperability requires RFC 4226 SHA-1.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/keywrap"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	mfaChallengeTTL  = 5 * time.Minute
	mfaEnrollmentTTL = 10 * time.Minute
	stepUpTTL        = 5 * time.Minute
	breakGlassTTL    = 15 * time.Minute
)

type TotpEnrollment struct {
	Token     string
	Secret    string
	URI       string
	ExpiresAt time.Time
}

type RecoveryCodes struct {
	Codes       []string
	GeneratedAt time.Time
}

func (service Service) BeginTOTPEnrollment(ctx context.Context, principal Principal, idempotencyKey string) (TotpEnrollment, error) {
	if service.KeyWrapping == nil {
		return TotpEnrollment{}, keywrap.ErrUnavailable
	}
	secretBytes := make([]byte, 20)
	if _, err := rand.Read(secretBytes); err != nil {
		return TotpEnrollment{}, err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secretBytes)
	encrypted, err := service.KeyWrapping.Encrypt(ctx, []byte(secret))
	if err != nil {
		return TotpEnrollment{}, err
	}
	token, err := RandomToken(32)
	if err != nil {
		return TotpEnrollment{}, err
	}
	now := service.now()
	username, _, err := service.identityStrings(ctx, principal.Session.Audience, principal.Session.UserID)
	if err != nil {
		return TotpEnrollment{}, err
	}
	issuer := "Argus"
	label := issuer + ":" + principal.Session.Audience + ":" + username
	uri := "otpauth://totp/" + url.PathEscape(label) + "?algorithm=SHA1&digits=6&issuer=" + url.QueryEscape(issuer) + "&period=30&secret=" + url.QueryEscape(secret)
	request := struct {
		Audience string `json:"audience"`
	}{Audience: principal.Session.Audience}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, principal.Session.Audience, principal.Session.UserID.String(),
		"identity.mfa.enroll", idempotencyKey, request, 201, func(queries *db.Queries) (TotpEnrollment, error) {
			if _, err := queries.UpsertMfaEnrollment(ctx, db.UpsertMfaEnrollmentParams{
				ID: mustUUIDV7(), Audience: principal.Session.Audience, UserID: principal.Session.UserID,
				Provider: encrypted.Provider, KeyID: encrypted.KeyID, KeyVersion: encrypted.KeyVersion,
				EncryptedSecret: encrypted.Value, EnrollmentHash: TokenHash(token),
				EnrollmentExpiresAt: pgtype.Timestamptz{Time: now.Add(mfaEnrollmentTTL), Valid: true},
			}); err != nil {
				return TotpEnrollment{}, err
			}
			return TotpEnrollment{Token: token, Secret: secret, URI: uri, ExpiresAt: now.Add(mfaEnrollmentTTL)}, nil
		})
}

func (service Service) VerifyTOTPEnrollment(ctx context.Context, principal Principal, enrollmentToken, code string) (RecoveryCodes, error) {
	credential, err := service.Store.Queries.GetMfaEnrollmentByHash(ctx, TokenHash(enrollmentToken))
	if err != nil || credential.Audience != principal.Session.Audience || credential.UserID != principal.Session.UserID {
		return RecoveryCodes{}, ErrMFAInvalid
	}
	secret, err := service.decryptMFASecret(ctx, credential)
	counter, valid := matchingTOTPCounter(secret, code, service.now())
	if err != nil || !valid {
		return RecoveryCodes{}, ErrMFAInvalid
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return RecoveryCodes{}, err
	}
	err = service.Store.InTx(ctx, func(queries *db.Queries) error {
		active, err := queries.ActivateMfaCredential(ctx, credential.ID)
		if err != nil {
			return err
		}
		if err := service.setMFAEnabled(ctx, queries, principal.Session.Audience, principal.Session.UserID, true); err != nil {
			return err
		}
		if err := replaceRecoveryCodes(ctx, queries, active.ID, hashes); err != nil {
			return err
		}
		rows, err := queries.ConsumeMfaTotpCounter(ctx, db.ConsumeMfaTotpCounterParams{ID: active.ID, LastTotpCounter: pgtype.Int8{Int64: counter, Valid: true}})
		if err != nil || rows != 1 {
			return ErrMFAInvalid
		}
		if err := queries.RevokeOtherSubjectSessions(ctx, db.RevokeOtherSubjectSessionsParams{Audience: principal.Session.Audience, UserID: principal.Session.UserID, ID: principal.Session.ID, RevokeReason: pgtype.Text{String: "mfa_enabled", Valid: true}}); err != nil {
			return err
		}
		return service.appendMFAAudit(ctx, queries, principal, "identity.mfa.enabled", "success", map[string]any{"priority": "high"})
	})
	return RecoveryCodes{Codes: codes, GeneratedAt: service.now()}, err
}

func (service Service) CompleteMFALogin(ctx context.Context, audience, challengeToken, code string) (IssuedSession, error) {
	var issued IssuedSession
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		challenge, err := queries.GetMfaChallengeByHash(ctx, TokenHash(challengeToken))
		if err != nil || challenge.Audience != audience || challenge.Purpose != "login" {
			return ErrMFAInvalid
		}
		method, err := service.verifyMFAWithQueries(ctx, queries, audience, challenge.UserID, code)
		if err != nil {
			return err
		}
		rows, err := queries.ConsumeMfaChallenge(ctx, challenge.ID)
		if err != nil || rows != 1 {
			return ErrMFAInvalid
		}
		issued, err = service.issueSessionWithAMR(ctx, queries, audience, challenge.UserID, []string{"password", method})
		return err
	})
	return issued, err
}

func (service Service) StepUp(ctx context.Context, principal Principal, code string) (time.Time, []string, error) {
	expires := service.now().Add(stepUpTTL)
	var amr []string
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		method, err := service.verifyMFAWithQueries(ctx, queries, principal.Session.Audience, principal.Session.UserID, code)
		if err != nil {
			return err
		}
		amr = uniqueStrings(append(principal.Session.Amr, method))
		_, err = queries.SetSessionStepUp(ctx, db.SetSessionStepUpParams{
			ID: principal.Session.ID, StepUpExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, Amr: amr,
		})
		return err
	})
	if err != nil {
		return time.Time{}, nil, err
	}
	return expires, amr, nil
}

func (service Service) RequireStepUp(principal Principal) error {
	if !principal.Session.StepUpExpiresAt.Valid || !service.now().Before(principal.Session.StepUpExpiresAt.Time) {
		return ErrStepUpRequired
	}
	return nil
}

func (service Service) RegenerateRecoveryCodes(ctx context.Context, principal Principal, proof string) (RecoveryCodes, error) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return RecoveryCodes{}, err
	}
	err = service.Store.InTx(ctx, func(queries *db.Queries) error {
		if _, err := service.verifyMFAWithQueries(ctx, queries, principal.Session.Audience, principal.Session.UserID, proof); err != nil {
			return err
		}
		credential, err := queries.GetMfaCredential(ctx, db.GetMfaCredentialParams{Audience: principal.Session.Audience, UserID: principal.Session.UserID})
		if err != nil || credential.Status != "active" {
			return ErrMFAInvalid
		}
		if err := replaceRecoveryCodes(ctx, queries, credential.ID, hashes); err != nil {
			return err
		}
		return service.appendMFAAudit(ctx, queries, principal, "identity.mfa.recovery_codes_regenerated", "success", map[string]any{"priority": "high"})
	})
	return RecoveryCodes{Codes: codes, GeneratedAt: service.now()}, err
}

func (service Service) DisableTOTP(ctx context.Context, principal Principal, proof string) error {
	if principal.PlatformUser != nil && principal.PlatformUser.Role == "platform_super_admin" {
		return ErrMFAEnrollment
	}
	return service.Store.InTx(ctx, func(queries *db.Queries) error {
		if _, err := service.verifyMFAWithQueries(ctx, queries, principal.Session.Audience, principal.Session.UserID, proof); err != nil {
			return err
		}
		rows, err := queries.DisableMfaCredential(ctx, db.DisableMfaCredentialParams{Audience: principal.Session.Audience, UserID: principal.Session.UserID})
		if err != nil || rows != 1 {
			return ErrMFAInvalid
		}
		if err := service.setMFAEnabled(ctx, queries, principal.Session.Audience, principal.Session.UserID, false); err != nil {
			return err
		}
		if err := queries.RevokeSubjectSessions(ctx, db.RevokeSubjectSessionsParams{Audience: principal.Session.Audience, UserID: principal.Session.UserID, RevokeReason: pgtype.Text{String: "mfa_disabled", Valid: true}}); err != nil {
			return err
		}
		if err := queries.RevokeSubjectBreakGlassSessions(ctx, principal.Session.UserID); err != nil {
			return err
		}
		return service.appendMFAAudit(ctx, queries, principal, "identity.mfa.disabled", "success", map[string]any{"priority": "high"})
	})
}

func (service Service) CreateBreakGlass(ctx context.Context, principal Principal, reason, ticketRef, idempotencyKey string) (db.BreakGlassSession, error) {
	if principal.EnterpriseUser == nil || len(strings.TrimSpace(reason)) < 8 || strings.TrimSpace(ticketRef) == "" {
		return db.BreakGlassSession{}, ErrMFAInvalid
	}
	if !service.BreakGlassEnabled {
		return db.BreakGlassSession{}, ErrBreakGlassDisabled
	}
	if err := service.RequireStepUp(principal); err != nil {
		return db.BreakGlassSession{}, err
	}
	expires := service.now().Add(breakGlassTTL)
	request := struct {
		Reason    string `json:"reason"`
		TicketRef string `json:"ticket_ref"`
	}{Reason: strings.TrimSpace(reason), TicketRef: strings.TrimSpace(ticketRef)}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", principal.Session.UserID.String(),
		"identity.break_glass.create", idempotencyKey, request, 201, func(queries *db.Queries) (db.BreakGlassSession, error) {
			created, err := queries.CreateBreakGlassSession(ctx, db.CreateBreakGlassSessionParams{
				ID: mustUUIDV7(), EnterpriseID: principal.EnterpriseIDValue(), UserID: principal.Session.UserID,
				SourceSessionID: principal.Session.ID, AuthorizationVersion: principal.AuthorizationVersion(),
				Reason: strings.TrimSpace(reason), TicketRef: strings.TrimSpace(ticketRef), ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
			})
			if err != nil {
				return db.BreakGlassSession{}, err
			}
			if err := service.appendMFAAudit(ctx, queries, principal, "identity.break_glass.created", "success", map[string]any{"priority": "high", "ticket_ref": created.TicketRef, "expires_at": expires}); err != nil {
				return db.BreakGlassSession{}, err
			}
			return created, nil
		})
}

func (service Service) ListBreakGlass(ctx context.Context, principal Principal) ([]db.BreakGlassSession, error) {
	if principal.EnterpriseUser == nil {
		return nil, ErrAudienceMismatch
	}
	return service.Store.Queries.ListBreakGlassSessions(ctx, db.ListBreakGlassSessionsParams{EnterpriseID: principal.EnterpriseIDValue(), UserID: principal.Session.UserID})
}

func (service Service) RevokeBreakGlass(ctx context.Context, principal Principal, id uuid.UUID) error {
	if principal.EnterpriseUser == nil {
		return ErrAudienceMismatch
	}
	return service.Store.InTx(ctx, func(queries *db.Queries) error {
		rows, err := queries.RevokeBreakGlassSession(ctx, db.RevokeBreakGlassSessionParams{ID: id, EnterpriseID: principal.EnterpriseIDValue(), UserID: principal.Session.UserID})
		if err != nil || rows != 1 {
			return pgx.ErrNoRows
		}
		return service.appendMFAAudit(ctx, queries, principal, "identity.break_glass.revoked", "success", map[string]any{"priority": "high", "break_glass_session_id": id.String()})
	})
}

func (service Service) createMFAChallenge(ctx context.Context, audience string, userID uuid.UUID, purpose string) (MFAChallenge, error) {
	token, err := RandomToken(32)
	if err != nil {
		return MFAChallenge{}, err
	}
	expires := service.now().Add(mfaChallengeTTL)
	_, err = service.Store.Queries.CreateMfaChallenge(ctx, db.CreateMfaChallengeParams{ID: mustUUIDV7(), ChallengeHash: TokenHash(token), Audience: audience, UserID: userID, Purpose: purpose, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}})
	return MFAChallenge{Token: token, Audience: audience, ExpiresAt: expires}, err
}

func (service Service) verifyMFAWithQueries(ctx context.Context, queries *db.Queries, audience string, userID uuid.UUID, proof string) (string, error) {
	credential, err := queries.GetMfaCredential(ctx, db.GetMfaCredentialParams{Audience: audience, UserID: userID})
	if err != nil || credential.Status != "active" {
		return "", ErrMFAEnrollment
	}
	secret, err := service.decryptMFASecret(ctx, credential)
	if err != nil {
		return "", err
	}
	if counter, valid := matchingTOTPCounter(secret, proof, service.now()); valid {
		rows, err := queries.ConsumeMfaTotpCounter(ctx, db.ConsumeMfaTotpCounterParams{ID: credential.ID, LastTotpCounter: pgtype.Int8{Int64: counter, Valid: true}})
		if err == nil && rows == 1 {
			return "totp", nil
		}
		return "", ErrMFAInvalid
	}
	rows, err := queries.ConsumeMfaRecoveryCode(ctx, db.ConsumeMfaRecoveryCodeParams{CredentialID: credential.ID, CodeHash: TokenHash(normalizeRecoveryCode(proof))})
	if err == nil && rows == 1 {
		return "recovery_code", nil
	}
	return "", ErrMFAInvalid
}

func (service Service) decryptMFASecret(ctx context.Context, credential db.MfaCredential) (string, error) {
	if service.KeyWrapping == nil {
		return "", keywrap.ErrUnavailable
	}
	plaintext, err := service.KeyWrapping.Decrypt(ctx, keywrap.Ciphertext{Provider: credential.Provider, KeyID: credential.KeyID, KeyVersion: credential.KeyVersion, Value: credential.EncryptedSecret})
	return string(plaintext), err
}

func (service Service) setMFAEnabled(ctx context.Context, queries *db.Queries, audience string, userID uuid.UUID, enabled bool) error {
	if audience == "platform" {
		return queries.SetPlatformMfaEnabled(ctx, db.SetPlatformMfaEnabledParams{ID: userID, MfaEnabled: enabled})
	}
	return queries.SetEnterpriseMfaEnabled(ctx, db.SetEnterpriseMfaEnabledParams{ID: userID, MfaEnabled: enabled})
}

func (service Service) appendMFAAudit(ctx context.Context, queries *db.Queries, principal Principal, actionName, result string, details map[string]any) error {
	domain := "platform"
	var enterpriseID uuid.NullUUID
	if id, ok := principal.EnterpriseID(); ok {
		domain, enterpriseID = "enterprise", uuid.NullUUID{UUID: id, Valid: true}
	}
	_, err := audit.Append(ctx, queries, audit.Entry{Domain: domain, EnterpriseID: enterpriseID, ActorType: principal.ActorType(), ActorID: principal.ActorID(), Action: actionName, ResourceType: "authentication", ResourceID: principal.ActorID(), Result: result, Details: details})
	return err
}

func replaceRecoveryCodes(ctx context.Context, queries *db.Queries, credentialID uuid.UUID, hashes [][]byte) error {
	if err := queries.DeleteMfaRecoveryCodes(ctx, credentialID); err != nil {
		return err
	}
	for _, hash := range hashes {
		if err := queries.CreateMfaRecoveryCode(ctx, db.CreateMfaRecoveryCodeParams{ID: mustUUIDV7(), CredentialID: credentialID, CodeHash: hash}); err != nil {
			return err
		}
	}
	return nil
}

func newRecoveryCodes() ([]string, [][]byte, error) {
	codes, hashes := make([]string, 10), make([][]byte, 10)
	for index := range codes {
		value := make([]byte, 10)
		if _, err := rand.Read(value); err != nil {
			return nil, nil, err
		}
		raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value)
		codes[index] = raw[:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
		hashes[index] = TokenHash(normalizeRecoveryCode(codes[index]))
	}
	return codes, hashes, nil
}

func normalizeRecoveryCode(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func verifyTOTP(secret, code string, now time.Time) bool {
	_, valid := matchingTOTPCounter(secret, code, now)
	return valid
}

func matchingTOTPCounter(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return 0, false
	}
	counter := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		message := make([]byte, 8)
		binary.BigEndian.PutUint64(message, uint64(counter+offset))
		mac := hmac.New(sha1.New, decoded)
		_, _ = mac.Write(message)
		digest := mac.Sum(nil)
		position := digest[len(digest)-1] & 0x0f
		value := (uint32(digest[position])&0x7f)<<24 | uint32(digest[position+1])<<16 | uint32(digest[position+2])<<8 | uint32(digest[position+3])
		expected := fmt.Sprintf("%06d", value%1_000_000)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return counter + offset, true
		}
	}
	return 0, false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func totpAt(secret string, at time.Time) string {
	decoded, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, decoded)
	_, _ = mac.Write(message)
	digest := mac.Sum(nil)
	position := digest[len(digest)-1] & 0x0f
	value := (uint32(digest[position])&0x7f)<<24 | uint32(digest[position+1])<<16 | uint32(digest[position+2])<<8 | uint32(digest[position+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}
