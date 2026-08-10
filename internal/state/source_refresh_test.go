package state

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestRefreshSecretSourceHashOnlyTouchesMatchingActiveSecret(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root, home := t.TempDir(), t.TempDir()
	active := secretBaseline("active", "", 1)
	active.SourcePath = "_secrets/active"
	dormant := secretBaseline("dormant", "", 2)
	dormant.SourcePath = "_darwin/_secrets/dormant"
	ordinary := ordinaryBaseline("ordinary", "", 3)
	ordinary.SourcePath = "ordinary"
	for _, baseline := range []FileBaseline{active, dormant, ordinary} {
		if _, err := store.UpsertFileBaseline(root, home, baseline); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RetireFileBaseline(root, home, "dormant"); err != nil {
		t.Fatal(err)
	}

	want := deployment.RawStorage([]byte("rotated"))
	changed, err := store.RefreshSecretSourceHash(root, home, "_secrets/active", want)
	if err != nil || !changed {
		t.Fatalf("refresh = %v, %v", changed, err)
	}
	for _, source := range []string{"_darwin/_secrets/dormant", "ordinary", "_secrets/missing"} {
		changed, err := store.RefreshSecretSourceHash(root, home, source, want)
		if err != nil || changed {
			t.Fatalf("refresh %s = %v, %v", source, changed, err)
		}
	}
	row, err := store.FileBaseline(root, home, "active")
	if err != nil || row.BaselineSourceHash != want {
		t.Fatalf("active row = %+v, %v", row, err)
	}
}

func TestRefreshSecretSourceHashIgnoresAbsentRepositoryBaseline(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	changed, err := store.RefreshSecretSourceHash(t.TempDir(), t.TempDir(), "_secrets/missing", sampleDigest(9))
	if err != nil || changed {
		t.Fatalf("refresh = %v, %v", changed, err)
	}
}
