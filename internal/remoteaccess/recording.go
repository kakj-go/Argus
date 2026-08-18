package remoteaccess

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	RecordingChunkBytes = 1 << 20
	RecordingFlushAfter = 5 * time.Second
	RecordingMaxBuffer  = 4 << 20
	RecordingStoreGrace = 30 * time.Second
)

var ErrRecordingUnavailable = errors.New("REMOTE_ACCESS_RECORDING_UNAVAILABLE")
var errRecordingDeferred = errors.New("recording ObjectStore write deferred")

type RecordingEvent struct {
	Time float64 `json:"time"`
	Type string  `json:"type"`
	Data any     `json:"data"`
}

type ObjectStore interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
}

type ChunkMetadata struct {
	Sequence     uint64
	ObjectKey    string
	Nonce        []byte
	EventCount   int
	CipherBytes  int
	PreviousHash [32]byte
	Hash         [32]byte
	StartedAt    time.Time
	EndedAt      time.Time
}

func DecryptChunk(recordingID string, dek, nonce, ciphertext []byte, sequence uint64, previousHash, expectedHash [32]byte) ([]RecordingEvent, error) {
	if len(dek) != 32 || recordingID == "" || sequence == 0 {
		return nil, ErrRecordingUnavailable
	}
	hashInput := append(append(previousHash[:0:0], previousHash[:]...), ciphertext...)
	if sha256.Sum256(hashInput) != expectedHash {
		return nil, ErrRecordingUnavailable
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	aad := make([]byte, 8+len(recordingID))
	binary.BigEndian.PutUint64(aad, sequence)
	copy(aad[8:], recordingID)
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrRecordingUnavailable
	}
	defer clear(plaintext)
	var result []RecordingEvent
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	for {
		var tuple []json.RawMessage
		if err := decoder.Decode(&tuple); errors.Is(err, io.EOF) {
			break
		} else if err != nil || len(tuple) != 3 {
			return nil, ErrRecordingUnavailable
		}
		var event RecordingEvent
		if json.Unmarshal(tuple[0], &event.Time) != nil || json.Unmarshal(tuple[1], &event.Type) != nil || json.Unmarshal(tuple[2], &event.Data) != nil {
			return nil, ErrRecordingUnavailable
		}
		if event.Type != "i" && event.Type != "o" && event.Type != "r" && event.Type != "m" {
			return nil, ErrRecordingUnavailable
		}
		result = append(result, event)
	}
	return result, nil
}

type Recorder struct {
	Store        ObjectStore
	RecordingID  string
	DEK          []byte
	Now          func() time.Time
	sequence     uint64
	previous     [32]byte
	buffer       []byte
	eventCount   int
	lastFlush    time.Time
	lastAttempt  time.Time
	failureSince time.Time
	chunkStarted time.Time
	chunkEnded   time.Time
}

func (recorder *Recorder) Append(ctx context.Context, event RecordingEvent) ([]ChunkMetadata, error) {
	if event.Type != "i" && event.Type != "o" && event.Type != "r" && event.Type != "m" {
		return nil, errors.New("invalid asciicast event type")
	}
	value, err := json.Marshal([]any{event.Time, event.Type, event.Data})
	if err != nil {
		return nil, err
	}
	value = append(value, '\n')
	if len(recorder.buffer)+len(value) > RecordingMaxBuffer {
		return nil, ErrRecordingUnavailable
	}
	recorder.buffer = append(recorder.buffer, value...)
	recorder.eventCount++
	now := recorder.now()
	if recorder.chunkStarted.IsZero() {
		recorder.chunkStarted = now
	}
	recorder.chunkEnded = now
	if recorder.lastFlush.IsZero() {
		recorder.lastFlush = now
	}
	if len(recorder.buffer) >= RecordingChunkBytes || now.Sub(recorder.lastFlush) >= RecordingFlushAfter {
		chunk, err := recorder.Flush(ctx)
		if errors.Is(err, errRecordingDeferred) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []ChunkMetadata{chunk}, nil
	}
	return nil, nil
}

// FlushDue retries a buffered ObjectStore write while the session remains
// active. A short outage is tolerated, but the session is terminated once the
// outage exceeds 30 seconds or the in-memory buffer reaches 4 MiB.
func (recorder *Recorder) FlushDue(ctx context.Context) ([]ChunkMetadata, error) {
	if len(recorder.buffer) == 0 {
		return nil, nil
	}
	now := recorder.now()
	if recorder.failureSince.IsZero() && now.Sub(recorder.lastFlush) < RecordingFlushAfter {
		return nil, nil
	}
	if !recorder.lastAttempt.IsZero() && now.Sub(recorder.lastAttempt) < time.Second {
		return nil, nil
	}
	chunk, err := recorder.Flush(ctx)
	if errors.Is(err, errRecordingDeferred) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []ChunkMetadata{chunk}, nil
}

func (recorder *Recorder) Flush(ctx context.Context) (ChunkMetadata, error) {
	if recorder.Store == nil || len(recorder.DEK) != 32 || recorder.RecordingID == "" || len(recorder.buffer) == 0 {
		return ChunkMetadata{}, ErrRecordingUnavailable
	}
	block, err := aes.NewCipher(recorder.DEK)
	if err != nil {
		return ChunkMetadata{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ChunkMetadata{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ChunkMetadata{}, err
	}
	sequence := recorder.sequence + 1
	aad := make([]byte, 8+len(recorder.RecordingID))
	binary.BigEndian.PutUint64(aad, sequence)
	copy(aad[8:], recorder.RecordingID)
	ciphertext := gcm.Seal(nil, nonce, recorder.buffer, aad)
	hashInput := append(append(recorder.previous[:0:0], recorder.previous[:]...), ciphertext...)
	hash := sha256.Sum256(hashInput)
	key := fmt.Sprintf("recordings/%s/%020d.cast.enc", recorder.RecordingID, sequence)
	recorder.lastAttempt = recorder.now()
	if err := recorder.Store.Put(ctx, key, ciphertext); err != nil {
		if recorder.failureSince.IsZero() {
			recorder.failureSince = recorder.lastAttempt
		}
		if recorder.lastAttempt.Sub(recorder.failureSince) >= RecordingStoreGrace || len(recorder.buffer) >= RecordingMaxBuffer {
			return ChunkMetadata{}, ErrRecordingUnavailable
		}
		return ChunkMetadata{}, errRecordingDeferred
	}
	metadata := ChunkMetadata{Sequence: sequence, ObjectKey: key, Nonce: nonce, EventCount: recorder.eventCount, CipherBytes: len(ciphertext), PreviousHash: recorder.previous, Hash: hash,
		StartedAt: recorder.chunkStarted, EndedAt: recorder.chunkEnded}
	recorder.sequence, recorder.previous = sequence, hash
	recorder.buffer = recorder.buffer[:0]
	recorder.eventCount = 0
	recorder.lastFlush = recorder.now()
	recorder.failureSince = time.Time{}
	recorder.chunkStarted, recorder.chunkEnded = time.Time{}, time.Time{}
	return metadata, nil
}

func (recorder *Recorder) now() time.Time {
	if recorder.Now != nil {
		return recorder.Now().UTC()
	}
	return time.Now().UTC()
}
