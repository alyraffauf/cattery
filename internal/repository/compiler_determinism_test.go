package repository

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func testPlanPayloadDeterminism(t *testing.T) {
	root := compileFixture(t)
	first, err := Compile(CompileInput{Platform: deployment.LayerDarwin, RepositoryRoot: root, HomeRoot: "/home/u"})
	if err != nil {
		t.Fatal(err)
	}
	writeRoutes(t, root, `version = 1

[symlinks.darwin]
".bashrc" = ["Bashrc"]

[symlinks.all]
".config/ghostty/config" = [".example/config"]
`)
	second, err := Compile(CompileInput{Platform: deployment.LayerDarwin, RepositoryRoot: root, HomeRoot: "/home/u"})
	if err != nil {
		t.Fatal(err)
	}
	assertPlan(t, first, second)
}
