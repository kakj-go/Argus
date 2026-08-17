package connector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	identityFile = "identity.json"
	keyFile      = "connector-key.pem"
	certFile     = "connector-cert.pem"
	caFile       = "connector-ca.pem"
	resultsFile  = "command-results.json"
)

type identityState struct {
	ConnectorID          string    `json:"connector_id"`
	Role                 string    `json:"role"`
	InstanceID           string    `json:"instance_id"`
	Name                 string    `json:"name"`
	GatewayEndpoint      string    `json:"gateway_endpoint"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`
	Capabilities         []string  `json:"capabilities"`
}

type commandRecord struct {
	CommandID      string    `json:"command_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Status         string    `json:"status"`
	ResultTypeURL  string    `json:"result_type_url,omitempty"`
	Result         []byte    `json:"result,omitempty"`
	ResultHash     string    `json:"result_hash,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type localStore struct{ directory string }

func (store localStore) ensure() error {
	if store.directory == "" {
		return errors.New("connector data directory is required")
	}
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return err
	}
	return os.Chmod(store.directory, 0o700)
}

func (store localStore) loadIdentity() (identityState, error) {
	var value identityState
	encoded, err := os.ReadFile(filepath.Join(store.directory, identityFile))
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(encoded, &value)
	if err != nil || value.ConnectorID == "" || value.InstanceID == "" || value.GatewayEndpoint == "" || len(value.Capabilities) == 0 {
		return identityState{}, errors.New("connector identity metadata is invalid")
	}
	return value, nil
}

func (store localStore) saveIdentity(value identityState, privateKey, certificate, caBundle []byte) error {
	if err := store.ensure(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	for path, content := range map[string][]byte{
		identityFile: encoded, keyFile: privateKey, certFile: certificate, caFile: caBundle,
	} {
		if len(content) == 0 {
			return errors.New("connector identity material is incomplete")
		}
		if err := atomicPrivateWrite(filepath.Join(store.directory, path), content); err != nil {
			return err
		}
	}
	return nil
}

func (store localStore) identityMaterial() (certificate, privateKey, caBundle []byte, err error) {
	certificate, err = os.ReadFile(filepath.Join(store.directory, certFile))
	if err != nil {
		return nil, nil, nil, err
	}
	privateKey, err = os.ReadFile(filepath.Join(store.directory, keyFile))
	if err != nil {
		return nil, nil, nil, err
	}
	caBundle, err = os.ReadFile(filepath.Join(store.directory, caFile))
	return certificate, privateKey, caBundle, err
}

func (store localStore) loadResults() (map[string]commandRecord, error) {
	encoded, err := os.ReadFile(filepath.Join(store.directory, resultsFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]commandRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := map[string]commandRecord{}
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func (store localStore) saveResult(value commandRecord) error {
	values, err := store.loadResults()
	if err != nil {
		return err
	}
	value.UpdatedAt = time.Now().UTC()
	if len(value.Result) > 0 {
		digest := sha256.Sum256(value.Result)
		value.ResultHash = hex.EncodeToString(digest[:])
	}
	values[value.CommandID] = value
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	for key, item := range values {
		if item.UpdatedAt.Before(cutoff) {
			delete(values, key)
		}
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateWrite(filepath.Join(store.directory, resultsFile), encoded)
}

func atomicPrivateWrite(path string, content []byte) error {
	temporary := path + ".tmp"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	remove = false
	return os.Chmod(path, 0o600)
}
