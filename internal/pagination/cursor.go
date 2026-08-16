package pagination

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid                   = errors.New("cursor invalid")
	ErrExpired                   = errors.New("cursor expired")
	ErrAuthorizationVersionStale = errors.New("cursor authorization version stale")
)

type Signer struct {
	Key []byte
	Now func() time.Time
}

type Binding struct {
	Audience             string
	EnterpriseID         string
	SubjectType          string
	SubjectID            string
	AuthorizationVersion int64
	FilterHash           string
	Sort                 string
}

type Position struct {
	Time time.Time
	ID   string
}

type payload struct {
	Version              int    `json:"version"`
	Audience             string `json:"audience"`
	EnterpriseID         string `json:"enterprise_id,omitempty"`
	SubjectType          string `json:"subject_type"`
	SubjectID            string `json:"subject_id"`
	AuthorizationVersion int64  `json:"authorization_version"`
	FilterHash           string `json:"filter_hash"`
	Sort                 string `json:"sort"`
	AfterTime            string `json:"after_time"`
	AfterID              string `json:"after_id"`
	ExpiresAt            int64  `json:"expires_at"`
}

func (signer Signer) Encode(binding Binding, position Position) (string, error) {
	if len(signer.Key) != 32 || position.ID == "" || position.Time.IsZero() {
		return "", ErrInvalid
	}
	body, err := json.Marshal(payload{
		Version: 1, Audience: binding.Audience, EnterpriseID: binding.EnterpriseID,
		SubjectType: binding.SubjectType, SubjectID: binding.SubjectID,
		AuthorizationVersion: binding.AuthorizationVersion, FilterHash: binding.FilterHash,
		Sort: binding.Sort, AfterTime: position.Time.UTC().Format(time.RFC3339Nano),
		AfterID: position.ID, ExpiresAt: signer.now().Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		return "", err
	}
	signature := signer.sign(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (signer Signer) Decode(value string, binding Binding) (Position, error) {
	if len(signer.Key) != 32 {
		return Position{}, ErrInvalid
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return Position{}, ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Position{}, ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, signer.sign(body)) {
		return Position{}, ErrInvalid
	}
	var decoded payload
	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Version != 1 {
		return Position{}, ErrInvalid
	}
	if signer.now().Unix() >= decoded.ExpiresAt {
		return Position{}, ErrExpired
	}
	if decoded.Audience != binding.Audience || decoded.EnterpriseID != binding.EnterpriseID ||
		decoded.SubjectType != binding.SubjectType || decoded.SubjectID != binding.SubjectID ||
		decoded.FilterHash != binding.FilterHash || decoded.Sort != binding.Sort {
		return Position{}, ErrInvalid
	}
	if decoded.AuthorizationVersion != binding.AuthorizationVersion {
		return Position{}, ErrAuthorizationVersionStale
	}
	afterTime, err := time.Parse(time.RFC3339Nano, decoded.AfterTime)
	if err != nil || decoded.AfterID == "" {
		return Position{}, ErrInvalid
	}
	return Position{Time: afterTime.UTC(), ID: decoded.AfterID}, nil
}

func HashFilter(value any) string {
	body, _ := json.Marshal(value)
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func (signer Signer) sign(body []byte) []byte {
	mac := hmac.New(sha256.New, signer.Key)
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func (signer Signer) now() time.Time {
	if signer.Now != nil {
		return signer.Now().UTC()
	}
	return time.Now().UTC()
}
