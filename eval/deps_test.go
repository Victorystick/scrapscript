package eval

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDependencies checks that the language packages reach outside the
// process only through what an Environment is given, so that they run
// anywhere, such as in WASM. Since fmt depends on os, only this module's own
// imports of the filesystem are checked, while the network is checked for
// transitively.
func TestDependencies(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f",
		`{{.ImportPath}}{{if not .Standard}} {{join .Imports " "}}{{end}}`, ".").Output()
	if err != nil {
		t.Fatal(err)
	}

	forbidden := map[string]bool{"net": true, "net/http": true, "github.com/Victorystick/scrapscript/yards": true}
	forbiddenImports := map[string]bool{"os": true, "os/exec": true, "path/filepath": true}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		pkg := fields[0]
		if forbidden[pkg] {
			t.Errorf("eval depends on %s", pkg)
		}
		for _, imp := range fields[1:] {
			if forbiddenImports[imp] {
				t.Errorf("%s imports %s", pkg, imp)
			}
		}
	}
}
