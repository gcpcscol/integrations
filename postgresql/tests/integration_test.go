//go:build integration

package tests

import (
	"context"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tcexec "github.com/testcontainers/testcontainers-go/exec"

	"github.com/testcontainers/testcontainers-go"
)

// plakarSHA is the plakar commit that is installed in the test image.
// Override via PLAKAR_SHA build arg when rebuilding the image manually.
const plakarSHA = "main"

// repoRoot returns the absolute path of the repository root by walking up from
// the test source file.  This is reliable regardless of where `go test` is
// invoked from.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

// execOK runs cmd inside container, streams the combined output to t.Log, and
// fails the test if the exit code is non-zero.
func execOK(ctx context.Context, t *testing.T, container testcontainers.Container, cmd ...string) {
	t.Helper()
	code, out, err := container.Exec(ctx, cmd, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("exec %v: %v", cmd, err)
	}
	b, _ := io.ReadAll(out)
	if s := strings.TrimSpace(string(b)); s != "" {
		t.Log(s)
	}
	if code != 0 {
		t.Fatalf("exec %v: exited %d", cmd, code)
	}
}

// TestInstallPlugin verifies that the postgresql integration can be built from
// source and installed into a running plakar instance.
func TestInstallPlugin(t *testing.T) {
	ctx := context.Background()
	root := repoRoot()
	sha := plakarSHA

	// Step 1 — build (or reuse) the plakar base image.
	// KeepImage:true ensures the built image is not removed after the test so
	// that Docker's layer cache makes subsequent runs fast.
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:       root,
			Dockerfile:    "tests/plakar.Dockerfile",
			BuildArgs:     map[string]*string{"PLAKAR_SHA": &sha},
			KeepImage:     true,
			PrintBuildLog: true,
		},
		// Keep the container alive while we exec commands inside it.
		Cmd: []string{"sleep", "infinity"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.BindMount(root, "/src"),
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	// Step 2 — build the plugin binaries from the mounted source, create the
	// .ptar package, and install it into this plakar instance.
	buildScript := `set -e
mkdir -p /tmp/pgpkg
cd /src
go build -o /tmp/pgpkg/postgresqlImporter    ./plugin/importer
go build -o /tmp/pgpkg/postgresqlExporter    ./plugin/exporter
go build -o /tmp/pgpkg/postgresqlBinImporter ./plugin/binimporter
cp /src/manifest.yaml /tmp/pgpkg/
cd /tmp/pgpkg
GOOS=$(go env GOOS)
GOARCH=$(go env GOARCH)
PTAR="postgresql_v0.0.1_${GOOS}_${GOARCH}.ptar"
rm -f "${PTAR}"
plakar pkg create ./manifest.yaml v0.0.1
plakar pkg add "./${PTAR}"`

	execOK(ctx, t, container, "sh", "-c", buildScript)

	// Step 3 — confirm the package shows up in the installed list.
	t.Log("=== plakar pkg list ===")
	execOK(ctx, t, container, "plakar", "pkg", "list")
}
