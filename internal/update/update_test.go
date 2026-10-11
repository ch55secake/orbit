package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/config"
)

func TestCommandRequiresValidVersionAndLocalAgent(t *testing.T) {
	target := config.Server{SSH: &config.SSHConfig{Host: "prod", RemotePort: 18787}}
	for _, version := range []string{"latest", "v1.2.3"} {
		cmd, err := Command(context.Background(), target, version)
		if err != nil {
			t.Fatal(err)
		}
		remote := cmd.Args[len(cmd.Args)-1]
		if !strings.Contains(remote, "sudo -- bash") || !strings.Contains(remote, "127.0.0.1:18787/api/v1/health") {
			t.Fatal("missing elevation or configured health port")
		}
	}
	if _, err := Command(context.Background(), target, "v1; echo bad"); err == nil {
		t.Fatal("invalid version accepted")
	}
	target.SSH.RemoteHost = "10.0.0.1"
	if _, err := Command(context.Background(), target, "latest"); err == nil {
		t.Fatal("different remote agent accepted")
	}
}

func TestRemoteUpdateTransactions(t *testing.T) {
	for _, platform := range []string{"Linux", "Darwin"} {
		for _, mode := range []string{"success", "checksum", "restart", "health", "incompatible"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				bin := filepath.Join(dir, "bin")
				if err := os.Mkdir(bin, 0700); err != nil {
					t.Fatal(err)
				}
				binary := filepath.Join(dir, "stardust")
				write := func(path, content string, mode os.FileMode) {
					t.Helper()
					if err := os.WriteFile(path, []byte(content), mode); err != nil {
						t.Fatal(err)
					}
				}
				write(binary, "old binary", 0755)
				write(filepath.Join(dir, "service-config"), "custom listener 18787", 0600)
				var archive bytes.Buffer
				gz := gzip.NewWriter(&archive)
				tarWriter := tar.NewWriter(gz)
				payload := "new binary"
				if err := tarWriter.WriteHeader(&tar.Header{Name: "stardust", Mode: 0755, Size: int64(len(payload))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tarWriter.Write([]byte(payload)); err != nil {
					t.Fatal(err)
				}
				if err := tarWriter.Close(); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
				archivePath := filepath.Join(dir, "release.tar.gz")
				if err := os.WriteFile(archivePath, archive.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
				if mode == "checksum" {
					hash = strings.Repeat("0", 64)
				}
				osName := "linux"
				if platform == "Darwin" {
					osName = "darwin"
				}
				checksums := filepath.Join(dir, "checksums")
				write(checksums, hash+"  stardust_1.2.3_"+osName+"_amd64.tar.gz\n", 0600)
				write(filepath.Join(bin, "id"), "#!/bin/sh\necho \"${MOCK_UID:-0}\"\n", 0700)
				write(filepath.Join(bin, "sudo"), "#!/bin/sh\n[ \"$1\" != -- ] || shift\nexport MOCK_UID=0\nexec \"$@\"\n", 0700)
				write(filepath.Join(bin, "uname"), "#!/bin/sh\nif [ \"$1\" = -s ]; then echo "+platform+"; else echo x86_64; fi\n", 0700)
				write(filepath.Join(bin, "PlistBuddy"), "#!/bin/sh\nprintf '%s\\n' \"$MOCK_BINARY\"\n", 0700)
				write(filepath.Join(bin, "systemctl"), `#!/bin/sh
case "$*" in
 *LoadState*) echo loaded ;;
 *ExecStart*) printf '{ path=%s ; argv[]=stardust --listen 127.0.0.1:18787 ; }\n' "$MOCK_BINARY" ;;
 restart*) echo restart >> "$MOCK_LOG"; [ "$MOCK_MODE" != restart ] || [ "$(wc -l < "$MOCK_LOG")" -gt 1 ] ;;
 *) exit 0 ;;
esac
`, 0700)
				write(filepath.Join(bin, "launchctl"), `#!/bin/sh
if [ "$1" = kickstart ]; then echo restart >> "$MOCK_LOG"; [ "$MOCK_MODE" != restart ] || [ "$(wc -l < "$MOCK_LOG")" -gt 1 ]; else echo 'pid = 123'; fi
`, 0700)
				write(filepath.Join(bin, "curl"), `#!/bin/sh
out=''
while [ "$#" -gt 0 ]; do
 if [ "$1" = --output ]; then out="$2"; shift 2; else url="$1"; shift; fi
done
if [ -z "$out" ]; then
 [ "$MOCK_MODE" != health ] || exit 1
 if [ "$MOCK_MODE" = incompatible ]; then printf '{"api_version":"v2","status":"ok"}'; exit 0; fi
 printf '{"api_version":"v1","status":"ok"}'
elif [ "${url##*/}" = checksums.txt ]; then cp "$MOCK_CHECKSUMS" "$out"
else cp "$MOCK_ARCHIVE" "$out"; fi
`, 0700)
				write(filepath.Join(bin, "sleep"), "#!/bin/sh\nexit 0\n", 0700)
				generated, err := Command(context.Background(), config.Server{SSH: &config.SSHConfig{Host: "prod", RemotePort: 18787}}, "latest")
				if err != nil {
					t.Fatal(err)
				}
				remote := strings.ReplaceAll(generated.Args[len(generated.Args)-1], "/usr/libexec/PlistBuddy", filepath.Join(bin, "PlistBuddy"))
				cmd := exec.Command("sh", "-c", remote)
				cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TMPDIR="+dir, "MOCK_BINARY="+binary, "MOCK_MODE="+mode, "MOCK_LOG="+filepath.Join(dir, "restarts"), "MOCK_ARCHIVE="+archivePath, "MOCK_CHECKSUMS="+checksums)
				if platform == "Darwin" {
					cmd.Env = append(cmd.Env, "MOCK_UID=1000")
				}
				output, err := cmd.CombinedOutput()
				if (err == nil) != (mode == "success") {
					t.Fatalf("unexpected result: %v\n%s", err, output)
				}
				got, err := os.ReadFile(binary)
				if err != nil {
					t.Fatal(err)
				}
				want := "old binary"
				if mode == "success" {
					want = payload
				}
				if string(got) != want {
					t.Fatalf("binary = %q, want %q\n%s", got, want, output)
				}
				cfg, err := os.ReadFile(filepath.Join(dir, "service-config"))
				if err != nil || string(cfg) != "custom listener 18787" {
					t.Fatal("service configuration modified")
				}
				leftovers, err := filepath.Glob(filepath.Join(dir, "orbit-stardust.*"))
				if err != nil || len(leftovers) != 0 {
					t.Fatalf("update artifacts left behind: %v", leftovers)
				}
				staged, err := filepath.Glob(filepath.Join(dir, ".stardust-update.*"))
				if err != nil || len(staged) != 0 {
					t.Fatalf("staging artifacts left behind: %v", staged)
				}
				restarts, _ := os.ReadFile(filepath.Join(dir, "restarts"))
				wantRestarts := 2
				if mode == "success" {
					wantRestarts = 1
				}
				if mode == "checksum" {
					wantRestarts = 0
				}
				if strings.Count(string(restarts), "restart\n") != wantRestarts {
					t.Fatalf("restart/rollback count = %q, want %d", restarts, wantRestarts)
				}
			})
		}
	}
}
