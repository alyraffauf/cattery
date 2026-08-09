package secrets

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/alyraffauf/cattery/internal/failure"
)

// ValidateCandidate round-trips candidate ciphertext through Decrypt and
// requires byte-exact equality with the original plaintext before the
// candidate may be adopted as repository content. The candidate must be
// nonempty valid JSON. Every plaintext buffer the round trip creates is
// zeroed on every path, and only the validated candidate bytes return.
func (client *Client) ValidateCandidate(ctx context.Context, plaintext []byte, candidate []byte, relative string) ([]byte, error) {
	if len(candidate) == 0 || !json.Valid(candidate) {
		return nil, failure.New(failure.Operational, "sops encrypt "+relative+" produced invalid candidate", nil)
	}
	decrypted, err := client.Decrypt(ctx, candidate, relative)
	if err != nil {
		return nil, err
	}
	match := bytes.Equal(decrypted, plaintext)
	zeroBytes(decrypted)
	if !match {
		return nil, failure.New(failure.Operational, "sops encrypt "+relative+" candidate differs from plaintext", nil)
	}
	return candidate, nil
}
