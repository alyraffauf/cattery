package secrets

import (
	"context"
	"encoding/json"
	"math"

	"github.com/alyraffauf/cattery/internal/failure"
)

// outputSlack is the allowance added to expected output sizes.
const outputSlack = 1024 * 1024

// Encrypt runs the sops encrypt shape against /dev/stdin with the
// repository-relative source name as the filename override. It requires
// nonempty valid JSON output, clears and rejects anything else, and never
// writes plaintext anywhere but the caller-supplied stdin. The returned
// ciphertext slice is caller-owned.
func (client *Client) Encrypt(ctx context.Context, plaintext []byte, relativePath string) ([]byte, error) {
	output, err := client.Run(ctx, Request{
		Operation:   "encrypt",
		SourcePath:  relativePath,
		Arguments:   encryptArguments(relativePath),
		Stdin:       plaintext,
		StdoutLimit: encryptLimit(len(plaintext)),
	})
	if err != nil {
		return nil, err
	}
	if len(output) == 0 || !json.Valid(output) {
		zeroBytes(output)
		return nil, failure.New(failure.Operational, "sops encrypt "+relativePath+" produced invalid JSON", nil)
	}
	return output, nil
}

// encryptArguments is the exact encryption invocation.
func encryptArguments(relativePath string) []string {
	return []string{
		"encrypt",
		"--filename-override", relativePath,
		"--input-type", "binary",
		"--output-type", "json",
		"/dev/stdin",
	}
}

// encryptLimit returns the encryption stdout bound, saturating at MaxInt so
// the doubled size plus slack can never wrap.
func encryptLimit(size int) int {
	if size > (math.MaxInt-outputSlack)/2 {
		return math.MaxInt
	}
	return 2*size + outputSlack
}
