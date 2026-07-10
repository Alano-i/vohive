package updater

import (
	"strings"
	"testing"
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
			{Name: "vohive_v0.1.2_linux_armv7", BrowserDownloadURL: "https://example.invalid/vohive_armv7"},
		},
	}

	asset, err := findReleaseAsset(release, "linux", "armv7")
	if err != nil {
		t.Fatalf("findReleaseAsset() unexpected error: %v", err)
	}
	if asset.Name != "vohive_v0.1.2_linux_armv7" {
		t.Fatalf("asset.Name = %q", asset.Name)
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
