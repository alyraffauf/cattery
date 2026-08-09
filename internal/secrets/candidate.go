package secrets

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/alyraffauf/cattery/internal/failure"
)

// Candidate pairs caller-owned plaintext with candidate ciphertext for
// adoption validation. ValidateCandidate never clears or retains Plaintext;
// the caller remains responsible for clearing it after this call.
type Candidate struct {
	Plaintext  []byte
	Ciphertext []byte
	SourcePath string
}

// ValidateCandidate round-trips candidate ciphertext through Decrypt and
// requires byte-exact equality with the original plaintext before the
// candidate may be adopted as repository content. The candidate must be
// nonempty valid JSON. Every plaintext buffer created by the round trip is
// zeroed before return or error, and only the validated ciphertext returns.
// The caller-owned Candidate.Plaintext is not modified.
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
