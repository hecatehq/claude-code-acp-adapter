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

func TestReleaseWorkflowVerifiesTagAndCleanModuleInputs(t *testing.T) {
	workflow := readRepoText(t, ".github/workflows/release.yml")
	for _, want := range []string{
		"Verify release tag",
		`git merge-base --is-ancestor "$release_commit" FETCH_HEAD`,
		`run: make release-check VERSION="${RELEASE_TAG#v}"`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q", want)
		}
	}

	makefile := readRepoText(t, "Makefile")
	for _, want := range []string{
		"tidy-check:",
		"$(GO) mod tidy -diff",
		"release-check: test test-race vet tidy-check version-smoke",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("makefile missing %q", want)
		}
	}

	goreleaser := readRepoText(t, ".goreleaser.yaml")
	if !strings.Contains(goreleaser, "- go mod tidy -diff") {
		t.Fatalf("goreleaser config must reject untidy module metadata")
	}
}

func TestReleaseDocsDescribeStableReadinessGate(t *testing.T) {
	release := readRepoText(t, "docs/RELEASE.md")
	for _, want := range []string{
		"GitHub artifact attestations",
		"gh attestation verify",
		"checksums.txt",
		"STABLE_READINESS.md",
		"git tag v0.3.1",
		"git push origin v0.3.1",
		"tag=v0.3.1",
	} {
		if !strings.Contains(release, want) {
			t.Fatalf("release docs missing %q", want)
		}
	}

	stable := readRepoText(t, "docs/STABLE_READINESS.md")
	for _, want := range []string{
		"Required Gate For Every Stable Release",
		"make release-check",
		"make real-cli-smoke",
		"Hecate compiles the versioned Go adapter library",
		"test-acp-real-embedded",
		"Parity Matrix",
		"Release artifacts",
		"gh attestation verify",
		"GitHub rules require pull-request review",
		"restrict `v*` tag creation, updates, and deletion",
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
