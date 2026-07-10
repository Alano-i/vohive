package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/iniwex5/vohive/internal/global"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/minio/selfupdate"
	"golang.org/x/mod/semver"
)

const (
	repoOwner = "Alano-i"
	repoName  = "vohive"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Body    string  `json:"body"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type UpdateInfo struct {
	HasUpdate   bool   `json:"has_update"`
	CurrentVer  string `json:"current_version"`
	LatestVer   string `json:"latest_version"`
	ReleaseNote string `json:"release_note"`
	IsDocker    bool   `json:"is_docker"`
	Platform    string `json:"platform"`
	AssetName   string `json:"asset_name"`
	DownloadURL string `json:"download_url"`
}

func latestReleaseURL() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)
}

func fetchLatestRelease() (*Release, error) {
	apiURL := latestReleaseURL()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request github api failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode response failed: %w", err)
	}
	return &release, nil
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func currentTargetPlatform() (string, string) {
	return runtime.GOOS, releaseArch(runtime.GOARCH)
}

func releaseArch(goarch string) string {
	if goarch == "arm" {
		return "armv7"
	}
	return goarch
}

func releaseAssetName(version, goos, arch string) string {
	return fmt.Sprintf("vohive_%s_%s_%s", version, goos, arch)
}

func findReleaseAsset(release *Release, goos, arch string) (Asset, error) {
	version := normalizeVersion(release.TagName)
	name := releaseAssetName(version, goos, arch)
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("no matching asset found for platform %s/%s, want %s", goos, arch, name)
}

// CheckUpdate 检查是否有新版本
func CheckUpdate() (*UpdateInfo, error) {
	release, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}

	currentVersion := normalizeVersion(global.Version)
	latestVersion := normalizeVersion(release.TagName)
	targetGoos, targetArch := currentTargetPlatform()
	asset, err := findReleaseAsset(release, targetGoos, targetArch)
	if err != nil {
		return nil, err
	}

	// 使用 semver 比较版本
	hasUpdate := false
	if semver.IsValid(currentVersion) && semver.IsValid(latestVersion) {
		if semver.Compare(currentVersion, latestVersion) < 0 {
			hasUpdate = true
		}
	} else {
		// 如果本地或线上不是标准 semver (比如 unknown, dev 等)，可以尝试直接不等即提示更新
		if currentVersion != latestVersion {
			hasUpdate = true
		}
	}

	isDocker := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		isDocker = true
	}

	return &UpdateInfo{
		HasUpdate:   hasUpdate,
		CurrentVer:  currentVersion,
		LatestVer:   latestVersion,
		ReleaseNote: release.Body,
		IsDocker:    isDocker,
		Platform:    fmt.Sprintf("%s/%s", targetGoos, targetArch),
		AssetName:   asset.Name,
		DownloadURL: asset.BrowserDownloadURL,
	}, nil
}

// ApplyUpdate 获取最新 release 并下载对应架构的二进制进行自我替换
func ApplyUpdate() error {
	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to fetch release info: %w", err)
	}

	targetGoos, targetArch := currentTargetPlatform()
	asset, err := findReleaseAsset(release, targetGoos, targetArch)
	if err != nil {
		return err
	}

	logger.Info("开始下载更新", "asset", asset.Name, "url", asset.BrowserDownloadURL)

	// 下载二进制
	client := &http.Client{Timeout: 5 * time.Minute}
	dlResp, err := client.Get(asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", dlResp.StatusCode)
	}

	// 执行替换
	err = selfupdate.Apply(dlResp.Body, selfupdate.Options{})
	if err != nil {
		// 回滚
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("update failed and rollback failed: %v, original error: %w", rerr, err)
		}
		return fmt.Errorf("update failed: %w", err)
	}

	logger.Info("应用更新成功，正在准备重启...")

	// 延迟退出以便接口能返回成功响应
	go func() {
		time.Sleep(2 * time.Second)
		if scheduleServiceRestart(exec.LookPath, exec.Command) {
			return
		}

		logger.Info("进程发出关闭信号以应用更新")
		if process, err := os.FindProcess(os.Getpid()); err == nil {
			process.Signal(syscall.SIGTERM)
		} else {
			os.Exit(0)
		}
	}()

	return nil
}

type commandStarter func(name string, arg ...string) *exec.Cmd

func scheduleServiceRestart(lookPath func(string) (string, error), command commandStarter) bool {
	systemctlPath, err := lookPath("systemctl")
	if err != nil {
		return false
	}

	if systemdRunPath, err := lookPath("systemd-run"); err == nil {
		cmd := command(
			systemdRunPath,
			"--unit=vohive-self-restart",
			"--description=Restart VoHive after self update",
			"--on-active=1s",
			systemctlPath,
			"restart",
			"vohive",
		)
		if err := cmd.Start(); err == nil {
			logger.Info("已通过 systemd-run 调度服务重启")
			return true
		} else {
			logger.Warn("systemd-run 调度服务重启失败，尝试直接调用 systemctl", "err", err)
		}
	}

	cmd := command(systemctlPath, "restart", "vohive")
	if err := cmd.Start(); err != nil {
		logger.Warn("调用 systemctl restart vohive 失败", "err", err)
		return false
	}
	logger.Info("已调用 systemctl restart vohive")
	return true
}
