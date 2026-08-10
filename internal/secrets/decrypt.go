package secrets

import (
	"context"
	"math"
)

// Decrypt runs the sops decrypt shape against /dev/stdin with the
// repository-relative source name as the filename override. The ciphertext
// bytes are caller-validated; sops never reopens a repository source path.
// The returned plaintext slice is caller-owned, and the client retains
// nothing after the call.
func (client *Client) Decrypt(ctx context.Context, ciphertext []byte, relativePath string) ([]byte, error) {
	return client.Run(ctx, Request{
		Operation:   "decrypt",
		SourcePath:  relativePath,
		Arguments:   decryptArguments(relativePath),
		Stdin:       ciphertext,
		StdoutLimit: decryptLimit(len(ciphertext)),
	})
}

// decryptArguments is the exact decryption invocation.
func decryptArguments(relativePath string) []string {
	return []string{
		"decrypt",
		"--filename-override", relativePath,
		"--input-type", "json",
		"--output-type", "binary",
		"/dev/stdin",
	}
}

// decryptLimit returns the decryption stdout bound, saturating at MaxInt so
// the size plus slack can never wrap.
func decryptLimit(size int) int {
	if size > math.MaxInt-outputSlack {
		return math.MaxInt
	}
	return size + outputSlack
}
