package deployment

import "testing"

func TestFingerprintVectors(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary of empty matches vector", testOrdinaryEmpty},
		{"ordinary equals raw storage", testOrdinaryRawStorageEqual},
		{"secret semantic differs from ordinary", testSecretSemanticDiffers},
		{"distinct keys distinct identifier", testIdentifierDistinctKeys},
		{"same key same identifier", testIdentifierSameKey},
		{"identifier is domain separated", testIdentifierDomainSeparated},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// ordinaryEmptyVector is the canonical BLAKE3-256 of the empty input.
var ordinaryEmptyVector = Digest{
	0xaf, 0x13, 0x49, 0xb9, 0xf5, 0xf9, 0xa1, 0xa6,
	0xa0, 0x40, 0x4d, 0xea, 0x36, 0xdc, 0xc9, 0x49,
	0x9b, 0xcb, 0x25, 0xc9, 0xad, 0xc1, 0x12, 0xb7,
	0xcc, 0x9a, 0x93, 0xca, 0xe4, 0x1f, 0x32, 0x62,
}

func testOrdinaryEmpty(t *testing.T) {
	got := Ordinary(nil)
	if got != ordinaryEmptyVector {
		t.Fatalf("Ordinary(empty) = %x, want %x", got, ordinaryEmptyVector)
	}
}

func testOrdinaryRawStorageEqual(t *testing.T) {
	payload := []byte("hello cattery")
	if Ordinary(payload) != RawStorage(payload) {
		t.Fatal("Ordinary and RawStorage must agree on identical bytes")
	}
}

func testSecretSemanticDiffers(t *testing.T) {
	plaintext := []byte("token")
	if SecretSemantic(plaintext, sampleKey(1)) == Ordinary(plaintext) {
		t.Fatal("keyed SecretSemantic must differ from unkeyed Ordinary")
	}
}

func testIdentifierDistinctKeys(t *testing.T) {
	a := HashKeyIdentifier(sampleKey(1))
	b := HashKeyIdentifier(sampleKey(2))
	if a == b {
		t.Fatal("distinct keys must yield distinct identifiers")
	}
}

func testIdentifierSameKey(t *testing.T) {
	key := sampleKey(7)
	first := HashKeyIdentifier(key)
	second := HashKeyIdentifier(key)
	if first != second {
		t.Fatal("same key must yield identical identifiers")
	}
}

func testIdentifierDomainSeparated(t *testing.T) {
	// HashKeyIdentifier mixes the literal domain separator into the digest, so
	// it must differ from Ordinary of the bare key bytes alone.
	key := sampleKey(3)
	bare := Ordinary(key[:])
	if HashKeyIdentifier(key) == bare {
		t.Fatal("HashKeyIdentifier must differ from Ordinary of the bare key")
	}
}

func sampleKey(seed byte) [32]byte {
	var key [32]byte
	for i := range key {
		key[i] = seed + byte(i)
	}
	return key
}
