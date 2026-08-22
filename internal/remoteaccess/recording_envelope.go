package remoteaccess

import (
	"encoding/json"

	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func recordingEnvelope(record db.RemoteAccessRecording) (secret.Envelope, error) {
	var envelope secret.Envelope
	if err := json.Unmarshal(record.WrappedDek, &envelope); err != nil ||
		envelope.Provider != record.KeyProvider ||
		envelope.KeyID != record.KeyID ||
		envelope.KeyVersion != int(record.KeyVersion) {
		return secret.Envelope{}, ErrRecordingUnavailable
	}
	return envelope, nil
}
