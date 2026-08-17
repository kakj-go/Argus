package automation

import "crypto/sha256"

func sha256Bytes(value []byte) []byte {
	hash := sha256.Sum256(value)
	return hash[:]
}
