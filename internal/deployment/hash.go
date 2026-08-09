package deployment

import "github.com/zeebo/blake3"

// Digest is a 32-byte BLAKE3 fingerprint exposed across package boundaries.
// The concrete blake3.Hasher type never leaves this package; callers receive
// only Digest values, keeping the BLAKE3 dependency pinned here.
type Digest [32]byte

// Ordinary returns the unkeyed BLAKE3 digest of an ordinary file's bytes for
// baseline comparison. Apply this only to non-secret content.
func Ordinary(bytes []byte) Digest {
	return blake3.Sum256(bytes)
}

// RawStorage returns the unkeyed BLAKE3 digest of an opaque encrypted secret
// payload as stored on disk. Callers must pass SOPS-encrypted bytes only;
// this package intentionally exposes no unkeyed plaintext-secret digest.
func RawStorage(bytes []byte) Digest {
	return blake3.Sum256(bytes)
}

// SecretSemantic returns the keyed BLAKE3 digest of secret plaintext under a
// per-installation 32-byte key. The keyed mode means low-entropy plaintext
// never compares equal to its unkeyed Ordinary digest.
func SecretSemantic(plaintext []byte, key [32]byte) Digest {
	hasher, err := blake3.NewKeyed(key[:])
	if err != nil {
		panic("deployment: blake3 NewKeyed rejected a 32-byte key")
	}
	_, _ = hasher.Write(plaintext)
	var out Digest
	_, _ = hasher.Digest().Read(out[:])
	return out
}

// HashKeyIdentifier returns a domain-separated unkeyed BLAKE3 digest that
// names a 32-byte secret key without exposing it. The digest covers the
// fixed literal "cattery/hash-key-id/v1\x00" prefixed to the key, so a key
// identifier cannot collide with any other unkeyed Cattery digest.
func HashKeyIdentifier(key [32]byte) Digest {
	hasher := blake3.New()
	_, _ = hasher.WriteString(domainSeparator)
	_, _ = hasher.Write(key[:])
	var out Digest
	_, _ = hasher.Digest().Read(out[:])
	return out
}

// domainSeparator is the literal prefix mixed into key identifiers so a
// Cattery key identifier cannot collide with any other unkeyed digest.
const domainSeparator = "cattery/hash-key-id/v1\x00"
