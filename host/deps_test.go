package host

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDependencies checks that the bindings' package needs no network, so
// that a host built for WebAssembly doesn't carry it. The command line,
// which fetches from the scrapyard, is in package cli.
func TestDependencies(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"net": true, "net/http": true, "github.com/Victorystick/scrapscript/yards": true}
	for _, pkg := range strings.Fields(string(out)) {
		if forbidden[pkg] {
			t.Errorf("host depends on %s", pkg)
		}
	}
}
