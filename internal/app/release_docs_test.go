package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowGeneratesProvenanceAttestations(t *testing.T) {
	workflow := readRepoText(t, ".github/workflows/release.yml")
	for _, want := range []string{
		"id-token: write",
		"attestations: write",
		"artifact-metadata: write",
		"uses: actions/attest@v4",
		"subject-checksums: ./dist/checksums.txt",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}
}

func TestReleaseDocsDescribeStableReadinessGate(t *testing.T) {
	release := readRepoText(t, "docs/RELEASE.md")
	for _, want := range []string{
		"GitHub artifact attestations",
		"gh attestation verify",
		"checksums.txt",
		"STABLE_READINESS.md",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release docs missing %q", want)
		}
	}

	stable := readRepoText(t, "docs/STABLE_READINESS.md")
	for _, want := range []string{
		"make release-check",
		"make real-cli-smoke",
		"Hecate ACP adapter release-binary smoke",
		"Parity Matrix",
		"Release artifacts",
		"gh attestation verify",
	} {
		if !strings.Contains(stable, want) {
			t.Fatalf("stable readiness docs missing %q", want)
		}
	}
}

func readRepoText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
