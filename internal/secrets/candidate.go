package secrets

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/alyraffauf/cattery/internal/failure"
)

// Candidate pairs the original plaintext with its candidate ciphertext for
// adoption validation.
type Candidate struct {
	Plaintext  []byte
	Ciphertext []byte
	SourcePath string
}

// ValidateCandidate round-trips candidate ciphertext through Decrypt and
// requires byte-exact equality with the original plaintext before the
// candidate may be adopted as repository content. The candidate must be
// nonempty valid JSON. Every plaintext buffer the round trip creates is
// zeroed on every path, and only the validated ciphertext returns.
func (client *Client) ValidateCandidate(ctx context.Context, candidate Candidate) ([]byte, error) {
	if len(candidate.Ciphertext) == 0 || !json.Valid(candidate.Ciphertext) {
		return nil, failure.New(failure.Operational, "sops encrypt "+candidate.SourcePath+" produced invalid candidate", nil)
	}
	decrypted, err := client.Decrypt(ctx, candidate.Ciphertext, candidate.SourcePath)
	if err != nil {
		return nil, err
	}
	match := bytes.Equal(decrypted, candidate.Plaintext)
	zeroBytes(decrypted)
	if !match {
		return nil, failure.New(failure.Operational, "sops encrypt "+candidate.SourcePath+" candidate differs from plaintext", nil)
	}
	return candidate.Ciphertext, nil
}
