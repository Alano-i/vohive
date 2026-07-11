package updater

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/selfupdate"
)

func TestLatestReleaseURLUsesCurrentRepository(t *testing.T) {
	got := latestReleaseURL()
	want := "https://api.github.com/repos/Alano-i/vohive/releases/latest"
	if got != want {
		t.Fatalf("latestReleaseURL() = %q, want %q", got, want)
	}
}

func TestReleaseArchMapsArmToArmv7(t *testing.T) {
	if got := releaseArch("arm"); got != "armv7" {
		t.Fatalf("releaseArch(arm) = %q, want armv7", got)
	}
	if got := releaseArch("arm64"); got != "arm64" {
		t.Fatalf("releaseArch(arm64) = %q, want arm64", got)
	}
}

func TestFindReleaseAssetMatchesCurrentNaming(t *testing.T) {
	release := &Release{
		TagName: "v0.1.2",
		Assets: []Asset{
			{Name: "install.sh", BrowserDownloadURL: "https://example.invalid/install.sh"},
			{
				Name:               "vohive_v0.1.2_linux_armv7",
				BrowserDownloadURL: "https://example.invalid/vohive_armv7",
				Digest:             "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			},
		},
	}

	asset, err := findReleaseAsset(release, "linux", "armv7")
	if err != nil {
		t.Fatalf("findReleaseAsset() unexpected error: %v", err)
	}
	if asset.Name != "vohive_v0.1.2_linux_armv7" {
		t.Fatalf("asset.Name = %q", asset.Name)
	}
	if asset.Digest != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("asset.Digest = %q", asset.Digest)
	}
}

func TestFindReleaseAssetReportsMissingPlatformPackage(t *testing.T) {
	release := &Release{
		TagName: "v0.1.2",
		Assets:  []Asset{{Name: "vohive_v0.1.2_linux_amd64"}},
	}

	_, err := findReleaseAsset(release, "linux", "arm64")
	if err == nil {
		t.Fatal("findReleaseAsset() expected error")
	}
	if !strings.Contains(err.Error(), "vohive_v0.1.2_linux_arm64") {
		t.Fatalf("error = %q, want missing asset name", err.Error())
	}
}

func TestVerifiedUpdateOptionsAcceptsSHA256Digest(t *testing.T) {
	payload := []byte("verified update")
	sum := sha256.Sum256(payload)
	asset := Asset{
		Name:   "vohive_v0.1.2_linux_arm64",
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
	}

	opts, err := verifiedUpdateOptions(asset)
	if err != nil {
		t.Fatalf("verifiedUpdateOptions() unexpected error: %v", err)
	}
	if opts.Hash != crypto.SHA256 {
		t.Fatalf("opts.Hash = %v, want SHA-256", opts.Hash)
	}
	if !bytes.Equal(opts.Checksum, sum[:]) {
		t.Fatalf("opts.Checksum = %x, want %x", opts.Checksum, sum)
	}
}

func TestVerifiedUpdateOptionsRejectsInvalidDigest(t *testing.T) {
	tests := []struct {
		name    string
		digest  string
		wantErr string
	}{
		{name: "missing", wantErr: "has no digest"},
		{name: "missing separator", digest: strings.Repeat("0", 64), wantErr: "invalid digest format"},
		{name: "unsupported algorithm", digest: "sha512:" + strings.Repeat("0", 128), wantErr: "unsupported digest algorithm"},
		{name: "invalid hex", digest: "sha256:" + strings.Repeat("z", 64), wantErr: "invalid SHA-256 digest"},
		{name: "short checksum", digest: "sha256:" + strings.Repeat("0", 62), wantErr: "invalid SHA-256 digest length"},
		{name: "trailing whitespace", digest: "sha256:" + strings.Repeat("0", 64) + " ", wantErr: "invalid SHA-256 digest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifiedUpdateOptions(Asset{Name: "vohive", Digest: tt.digest})
			if err == nil {
				t.Fatal("verifiedUpdateOptions() expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestVerifiedUpdateOptionsProtectsBinaryReplacement(t *testing.T) {
	const targetName = "vohive"
	original := []byte("current binary")
	update := []byte("new binary")

	tests := []struct {
		name        string
		digestFor   []byte
		wantErr     bool
		wantContent []byte
	}{
		{name: "matching digest", digestFor: update, wantContent: update},
		{name: "mismatched digest", digestFor: []byte("different binary"), wantErr: true, wantContent: original},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetPath := filepath.Join(t.TempDir(), targetName)
			if err := os.WriteFile(targetPath, original, 0o755); err != nil {
				t.Fatalf("write target: %v", err)
			}

			sum := sha256.Sum256(tt.digestFor)
			opts, err := verifiedUpdateOptions(Asset{
				Name:   targetName,
				Digest: "sha256:" + hex.EncodeToString(sum[:]),
			})
			if err != nil {
				t.Fatalf("verifiedUpdateOptions() unexpected error: %v", err)
			}
			opts.TargetPath = targetPath

			err = selfupdate.Apply(bytes.NewReader(update), opts)
			if tt.wantErr && err == nil {
				t.Fatal("selfupdate.Apply() expected checksum error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("selfupdate.Apply() unexpected error: %v", err)
			}

			got, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			if !bytes.Equal(got, tt.wantContent) {
				t.Fatalf("target content = %q, want %q", got, tt.wantContent)
			}

			stagedPath := filepath.Join(filepath.Dir(targetPath), "."+targetName+".new")
			if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
				t.Fatalf("staged update remains at %s", stagedPath)
			}
		})
	}
}
