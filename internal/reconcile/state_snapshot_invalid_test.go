package reconcile

import (
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/state"
)

func invalidFileVariants() []state.FileBaseline {
	absolute := fileRow(".config/app", "apps", "files/app")
	absolute.TargetPath = "/etc/passwd"
	backslash := fileRow(".config/app", "apps", "files/app")
	backslash.SourcePath = `files\app`
	badKind := fileRow(".config/app", "apps", "files/app")
	badKind.SourceKind = "unknown"
	badLayer := fileRow(".config/app", "apps", "files/app")
	badLayer.Layer = "windows"
	unset := fileRow(".config/app", "apps", "files/app")
	unset.BaselineContentHash = deployment.Digest{}
	badBits := fileRow(".config/app", "apps", "files/app")
	badBits.ExecutableBits = 0o1000
	badStatus := fileRow(".config/app", "apps", "files/app")
	badStatus.Status = "gone"
	activeRetired := fileRow(".config/app", "apps", "files/app")
	activeRetired.RetiredAt = ptrTimestamp()
	retiredMissing := fileRow(".config/app", "apps", "files/app")
	retiredMissing.Status = state.StatusRetired
	return []state.FileBaseline{absolute, backslash, badKind, badLayer, unset, badBits, badStatus, activeRetired, retiredMissing}
}

func invalidAliasVariants() []state.AliasBaseline {
	absolute := aliasRow(".bin/x", "files/x", "")
	absolute.AliasPath = "/etc/rc"
	badTarget := aliasRow(".bin/x", "files/x", "")
	badTarget.CanonicalTargetPath = "/etc/rc"
	badLayer := aliasRow(".bin/x", "files/x", "")
	badLayer.Layer = "windows"
	badStatus := aliasRow(".bin/x", "files/x", "")
	badStatus.Status = "gone"
	activeRetired := aliasRow(".bin/x", "files/x", "")
	activeRetired.RetiredAt = ptrTimestamp()
	retiredMissing := aliasRow(".bin/x", "files/x", "")
	retiredMissing.Status = state.StatusRetired
	return []state.AliasBaseline{absolute, badTarget, badLayer, badStatus, activeRetired, retiredMissing}
}

func ptrTimestamp() *time.Time {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &when
}
