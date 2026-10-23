package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alireza0/s-ui/config"
	"github.com/alireza0/s-ui/logger"
)

type PanelService struct {
}

func (s *PanelService) RestartPanel(delay time.Duration) error {
	p, err := os.FindProcess(syscall.Getpid())
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(delay)
		if runtime.GOOS == "windows" {
			err = p.Kill()
		} else {
			err = p.Signal(syscall.SIGHUP)
		}
		if err != nil {
			logger.Error("send signal SIGHUP failed:", err)
		}
	}()
	return nil
}

// PanelUpdateInfo 是面板版本检查的结果。
type PanelUpdateInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

const (
	// s-ui 没有独立的 update.sh,升级就是重跑 install.sh。这样做是安全的:
	// 脚本里只有在 /usr/local/s-ui/db/s-ui.db 不存在时才生成随机账号密码和端口,
	// 已装过的机器走的是「保留现有设置」那条路 —— 否则 Web 点一下更新就会
	// 把管理员锁在面板外面。
	panelUpdaterURL      = "https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh"
	panelReleaseAPI      = "https://api.github.com/repos/Teminuosi/s-ui/releases/latest"
	maxPanelUpdaterBytes = 2 << 20
)

// GetUpdateInfo 查 GitHub 上的最新 release,和本机版本比较。
func (s *PanelService) GetUpdateInfo() (*PanelUpdateInfo, error) {
	latest, err := fetchLatestPanelVersion()
	if err != nil {
		return nil, err
	}
	current := config.GetVersion()
	return &PanelUpdateInfo{
		CurrentVersion:  current,
		LatestVersion:   latest,
		UpdateAvailable: isNewerVersion(latest, current),
	}, nil
}

// StartUpdate 在当前请求之外把更新跑起来。
//
// 关键是让更新进程活过面板自己 —— 脚本装完会重启 s-ui,如果更新进程是面板的
// 子进程,它会跟着一起死,留下装了一半的状态。优先用 systemd-run 起一个独立
// 单元;拿不到 systemd 就退回 setsid 脱离进程组。
// 已经在更新就别再起第二个:点了「更新」按钮还能再点(或开着两个页面各点一次),
// 两份 install.sh 同时跑会互相覆盖二进制、抢着重启服务。
// 更新成功后面板会以新进程重启,这个记录自然清零;10 分钟内没重启说明更新卡住了,放行重试。
var (
	updateMu      sync.Mutex
	updateStarted time.Time
)

const updateGuard = 10 * time.Minute

func (s *PanelService) StartUpdate() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("panel web update is supported only on Linux installations")
	}
	updateMu.Lock()
	defer updateMu.Unlock()
	if !updateStarted.IsZero() && time.Since(updateStarted) < updateGuard {
		return fmt.Errorf("更新已经在进行中(%d 秒前开始),面板会自行重启,请等 1-2 分钟后刷新页面", int(time.Since(updateStarted).Seconds()))
	}
	if err := s.startUpdateJob(); err != nil {
		return err
	}
	updateStarted = time.Now()
	return nil
}

func (s *PanelService) startUpdateJob() error {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("bash is required to run the panel updater: %w", err)
	}

	scriptPath, err := downloadPanelUpdater()
	if err != nil {
		return err
	}

	// 跑完删掉临时脚本,不管成败。SUI_AUTO=1 让它全程不问问题。
	updateScript := fmt.Sprintf("set -e; trap 'rm -f %s' EXIT; SUI_AUTO=1 %s %s",
		shellQuote(scriptPath), shellQuote(bash), shellQuote(scriptPath))

	if systemdRun, err := exec.LookPath("systemd-run"); err == nil {
		unitName := fmt.Sprintf("s-ui-web-update-%d", time.Now().Unix())
		cmd := exec.Command(systemdRun, "--unit", unitName, bash, "-lc", updateScript)
		out, err := cmd.CombinedOutput()
		if err != nil {
			output := strings.TrimSpace(string(out))
			// 容器或没跑 systemd 的环境:不是错误,退到 setsid 那条路
			if !strings.Contains(output, "System has not been booted with systemd") &&
				!strings.Contains(output, "Failed to connect to bus") {
				_ = os.Remove(scriptPath)
				return fmt.Errorf("failed to start panel update job: %w: %s", err, output)
			}
			logger.Warning("systemd-run unavailable, falling back to detached update process:", output)
		} else {
			logger.Info("started panel update job via systemd-run unit ", unitName)
			return nil
		}
	}

	cmd := exec.Command(bash, "-lc", "setsid "+updateScript+" >/dev/null 2>&1 </dev/null &")
	if err := cmd.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("failed to start panel update job: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		logger.Warning("failed to release panel update process:", err)
	}
	logger.Info("started detached panel update job")
	return nil
}

func downloadPanelUpdater() (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(panelUpdaterURL)
	if err != nil {
		return "", fmt.Errorf("download panel updater: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download panel updater: unexpected HTTP %d", resp.StatusCode)
	}

	file, err := os.CreateTemp("", "s-ui-update-*.sh")
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()

	n, err := io.Copy(file, io.LimitReader(resp.Body, maxPanelUpdaterBytes+1))
	if err != nil {
		return "", fmt.Errorf("write panel updater: %w", err)
	}
	if n > maxPanelUpdaterBytes {
		return "", fmt.Errorf("panel updater exceeds %d bytes", maxPanelUpdaterBytes)
	}
	if err := file.Chmod(0700); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func fetchLatestPanelVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(panelReleaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("latest panel release tag is empty")
	}
	return release.TagName, nil
}

// 版本形如 1.4.2-qs43(CI 会把 tag 去掉 v 后写进 config/version)。
// 先比三段主版本号,一样再比 qs 序号 —— 光比字符串的话,本地跑着比线上新的
// 开发版时会一直亮「有更新」。
func isNewerVersion(latest, current string) bool {
	lMain, lQs, lOk := parseSuiVersion(latest)
	cMain, cQs, cOk := parseSuiVersion(current)
	if !lOk || !cOk {
		return normalizeVersionTag(latest) != normalizeVersionTag(current)
	}
	for i := range lMain {
		if lMain[i] != cMain[i] {
			return lMain[i] > cMain[i]
		}
	}
	return lQs > cQs
}

// 返回三段主版本号、qs 序号(没有则 0)、是否解析成功。
func parseSuiVersion(version string) ([3]int, int, bool) {
	var main [3]int
	v := normalizeVersionTag(version)
	qs := 0
	if idx := strings.Index(v, "-qs"); idx >= 0 {
		if n, err := strconv.Atoi(v[idx+3:]); err == nil {
			qs = n
		}
		v = v[:idx]
	} else if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return main, 0, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return main, 0, false
		}
		main[i] = n
	}
	return main, qs, true
}

func normalizeVersionTag(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
