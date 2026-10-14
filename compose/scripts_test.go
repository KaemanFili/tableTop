package compose_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the archive workflow without connecting to a real Docker daemon or
// changing the user's running service. The stub executes real tar commands.
func scriptFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	source, destination := filepath.Join(root, "source"), filepath.Join(root, "destination")
	os.MkdirAll(source, 0700)
	os.MkdirAll(destination, 0700)
	stub := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$TABLETOP_TEST_LOG"
if [ "$1" = inspect ]; then printf 'true false\n'; exit 0; fi
[ "$1" = compose ]
shift
while [ "${1:-}" = -f ]; do shift 2; done
case "$1" in
ps)
  case "$*" in *--status*) exit 0;; esac
  printf 'test-container\n';;
pause|unpause|create|config) exit 0;;
run)
  if [ "${TABLETOP_TEST_FAIL_BACKUP:-}" = true ]; then exit 1; fi
  case "$*" in
    *'tar -czf'*) tar -czf - -C "$TABLETOP_TEST_SOURCE" .;;
    *'sh -eu -c'*)
      [ -z "$(ls -A "$TABLETOP_TEST_DESTINATION")" ] || exit 1
      tar -xzf - -C "$TABLETOP_TEST_DESTINATION";;
    *) exit 1;;
  esac;;
*) exit 1;;
esac
`
	os.WriteFile(filepath.Join(root, "docker"), []byte(stub), 0700)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TABLETOP_HOST_DATA", "0")
	t.Setenv("TABLETOP_TEST_LOG", filepath.Join(root, "docker.log"))
	t.Setenv("TABLETOP_TEST_SOURCE", source)
	t.Setenv("TABLETOP_TEST_DESTINATION", destination)
	return root, source, destination
}

func TestBackupRestoreAndFailureCleanup(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("restore validation requires python3")
	}
	root, source, destination := scriptFixture(t)
	os.MkdirAll(filepath.Join(source, "sessions", "game"), 0700)
	payload := []byte(`{"members":["123"]}`)
	os.WriteFile(filepath.Join(source, "access.json"), payload, 0600)
	os.WriteFile(filepath.Join(source, "sessions", "game", "maps.json"), []byte("[]\n"), 0600)
	archive := filepath.Join(root, "backup.tar.gz")
	if output, err := exec.Command("sh", "backup.sh", archive).CombinedOutput(); err != nil {
		t.Fatalf("backup: %v %s", err, output)
	}
	if output, err := exec.Command("sh", "restore.sh", archive).CombinedOutput(); err != nil {
		t.Fatalf("restore: %v %s", err, output)
	}
	data, _ := os.ReadFile(filepath.Join(destination, "access.json"))
	if string(data) != string(payload) {
		t.Fatal("backup roundtrip changed enrollment data")
	}
	if _, err := os.Stat(filepath.Join(destination, "sessions", "game", "maps.json")); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("sh", "restore.sh", archive).Run(); err == nil {
		t.Fatal("restore overwrote a non-empty destination")
	}
	t.Setenv("TABLETOP_TEST_FAIL_BACKUP", "true")
	failedArchive := filepath.Join(root, "failed.tar.gz")
	if err := exec.Command("sh", "backup.sh", failedArchive).Run(); err == nil {
		t.Fatal("failed backup reported success")
	}
	if _, err := os.Stat(failedArchive); !os.IsNotExist(err) {
		t.Fatal("failed backup left a final archive")
	}
	log, _ := os.ReadFile(filepath.Join(root, "docker.log"))
	if strings.Count(string(log), "unpause tabletop") != 2 {
		t.Fatal("backup failed to unpause the service")
	}
}

func TestRestoreRejectsUnsafeArchives(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("restore validation requires python3")
	}
	root, _, _ := scriptFixture(t)
	archive := filepath.Join(root, "unsafe.tar.gz")
	file, _ := os.Create(archive)
	compressed := gzip.NewWriter(file)
	writer := tar.NewWriter(compressed)
	writer.WriteHeader(&tar.Header{Name: "../escape", Mode: 0600, Size: 1})
	writer.Write([]byte("x"))
	writer.Close()
	compressed.Close()
	file.Close()
	if output, err := exec.Command("sh", "restore.sh", archive).CombinedOutput(); err == nil || !strings.Contains(string(output), "unsafe path") {
		t.Fatalf("unsafe archive was not rejected: %v %s", err, output)
	}
}

func TestComposeDevFlagIsExplicit(t *testing.T) {
	root, _, _ := scriptFixture(t)
	for _, args := range [][]string{{"compose.sh", "config"}, {"compose.sh", "-dev", "config"}} {
		if output, err := exec.Command("sh", args...).CombinedOutput(); err != nil {
			t.Fatalf("compose wrapper: %v %s", err, output)
		}
	}
	log, _ := os.ReadFile(filepath.Join(root, "docker.log"))
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "compose.dev.yaml") || !strings.Contains(lines[1], "compose.dev.yaml") {
		t.Fatal("development override was not controlled by -dev")
	}
}

func TestComposeTunnelFlagAndDevelopmentRejection(t *testing.T) {
	root, _, _ := scriptFixture(t)
	for _, args := range [][]string{
		{"compose.sh", "config"},
		{"compose.sh", "-tunnel", "config"},
		{"compose.sh", "-host-data", "-tunnel", "config"},
	} {
		if output, err := exec.Command("sh", args...).CombinedOutput(); err != nil {
			t.Fatalf("compose wrapper: %v %s", err, output)
		}
	}
	log, err := os.ReadFile(filepath.Join(root, "docker.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 3 || strings.Contains(lines[0], "compose.tunnel.yaml") ||
		!strings.Contains(lines[1], "compose.tunnel.yaml") ||
		!strings.Contains(lines[2], "compose.host-data.yaml") || !strings.Contains(lines[2], "compose.tunnel.yaml") {
		t.Fatal("tunnel or host storage overrides were not selected correctly")
	}
	for _, args := range [][]string{
		{"compose.sh", "-dev", "-tunnel", "config"},
		{"compose.sh", "-tunnel", "-dev", "config"},
	} {
		if output, err := exec.Command("sh", args...).CombinedOutput(); err == nil || !strings.Contains(string(output), "unauthenticated") {
			t.Fatalf("development tunnel was not rejected: %v %s", err, output)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "docker.log"))
	if string(after) != string(log) {
		t.Fatal("rejected development tunnel still invoked Docker")
	}
}
