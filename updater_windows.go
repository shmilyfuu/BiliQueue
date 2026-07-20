//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type updateRestartSpec struct {
	Args       []string `json:"args"`
	WorkingDir string   `json:"workingDir"`
}

func validateSelfUpdateTarget() error {
	executable, err := updateExecutablePath()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Base(executable), "BiliQueue-windows-amd64.exe") {
		return errors.New("源码运行和测试程序只支持检查更新，不能自动替换；请在正式发布版中使用自动更新")
	}
	return nil
}

func launchUpdateHelper(app *App, prepared preparedUpdate) error {
	target, err := updateExecutablePath()
	if err != nil {
		return err
	}
	workingDir, _ := os.Getwd()
	spec := updateRestartSpec{Args: filterUpdaterArgs(os.Args[1:]), WorkingDir: workingDir}
	if err := startUpdateHelper(prepared, target, spec); err != nil {
		return err
	}
	discardDeferredUpdate()
	go func() {
		time.Sleep(900 * time.Millisecond)
		if tray := getActiveTray(); tray != nil && tray.hwnd != 0 {
			procPostMessageW.Call(tray.hwnd, wmClose, 0, 0)
			return
		}
		app.prepareExit()
		if app.serverControl != nil {
			_ = app.serverControl.Close()
		}
		os.Exit(0)
	}()
	return nil
}

func startUpdateHelper(prepared preparedUpdate, target string, spec updateRestartSpec) error {
	specPath := filepath.Join(prepared.Root, "restart.json")
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err := os.WriteFile(specPath, data, 0o600); err != nil {
		return err
	}
	cmd := exec.Command(prepared.HelperEXE,
		"-update-helper",
		"-update-target", target,
		"-update-package-root", prepared.PackageRoot,
		"-update-restart-file", specPath,
		"-update-parent-pid", strconv.Itoa(os.Getpid()),
	)
	cmd.Dir = prepared.PackageRoot
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新助手：%w", err)
	}
	return nil
}

func notifyUpdateAvailable(app *App, info UpdateInfo) {
	message := fmt.Sprintf("检测到新版本 v%s（%s）。\n\n是否现在下载更新包？下载不会中断当前直播。", info.Version, info.Source)
	if !showStyledConfirmDialog("发现新版本", message) {
		return
	}
	if _, err := app.downloadLatestUpdate(context.Background()); err != nil {
		showErrorDialog("更新失败", err.Error())
		return
	}
	if showStyledChoiceDialog("更新已下载", "更新包已经下载并解压完成。请选择更新时间。", "立即更新", "下次启动时") {
		if err := app.applyPreparedUpdate(); err != nil {
			showErrorDialog("更新失败", err.Error())
		}
		return
	}
	if _, err := app.deferPreparedUpdate(); err != nil {
		showErrorDialog("更新失败", err.Error())
	}
}

func launchDeferredUpdateIfPresent() (bool, error) {
	prepared, exists, err := loadDeferredUpdate()
	if err != nil || !exists {
		return false, err
	}
	if err := validateSelfUpdateTarget(); err != nil {
		return false, err
	}
	target, err := updateExecutablePath()
	if err != nil {
		return false, err
	}
	workingDir, _ := os.Getwd()
	spec := updateRestartSpec{Args: filterUpdaterArgs(os.Args[1:]), WorkingDir: workingDir}
	if err := startUpdateHelper(prepared, target, spec); err != nil {
		return false, err
	}
	discardDeferredUpdate()
	return true, nil
}

func runUpdateHelper(target, packageRoot string, parentPID int, restartFile string) error {
	_ = parentPID
	target = filepath.Clean(target)
	packageRoot = filepath.Clean(packageRoot)
	if filepath.Base(target) == "." || !strings.EqualFold(filepath.Base(target), "BiliQueue-windows-amd64.exe") {
		return errors.New("更新目标不是 BiliQueue 正式版主程序")
	}
	source := filepath.Join(packageRoot, "BiliQueue-windows-amd64.exe")
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("更新包主程序无效：%w", err)
	}
	var spec updateRestartSpec
	if data, err := os.ReadFile(restartFile); err == nil {
		_ = json.Unmarshal(data, &spec)
	}
	backup := target + ".old"
	temporary := target + ".new"
	_ = os.Remove(backup)
	_ = os.Remove(temporary)
	var renameErr error
	for attempt := 0; attempt < 120; attempt++ {
		renameErr = os.Rename(target, backup)
		if renameErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if renameErr != nil {
		return fmt.Errorf("等待旧程序退出并备份失败：%w", renameErr)
	}
	restore := func() {
		_ = os.Remove(temporary)
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
	}
	if err := copyUpdateFile(source, temporary, 0o755); err != nil {
		restore()
		return fmt.Errorf("写入新程序：%w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		restore()
		return fmt.Errorf("替换主程序：%w", err)
	}
	if err := copyReleaseSupportFiles(packageRoot, filepath.Dir(target)); err != nil {
		restore()
		return fmt.Errorf("更新随包资源：%w", err)
	}
	args := append([]string{}, spec.Args...)
	args = append(args, "-update-cleanup-root", filepath.Dir(filepath.Clean(restartFile)), "-update-cleanup-backup", backup)
	cmd := exec.Command(target, args...)
	if spec.WorkingDir != "" {
		cmd.Dir = spec.WorkingDir
	} else {
		cmd.Dir = filepath.Dir(target)
	}
	if err := cmd.Start(); err != nil {
		restore()
		return fmt.Errorf("重启 BiliQueue：%w", err)
	}
	return nil
}

func copyReleaseSupportFiles(sourceRoot, targetRoot string) error {
	for _, name := range []string{"README.md", "start-BiliQueue.cmd"} {
		source := filepath.Join(sourceRoot, name)
		if _, err := os.Stat(source); err == nil {
			if err := copyUpdateFile(source, filepath.Join(targetRoot, name), 0o644); err != nil {
				return err
			}
		}
	}
	assetsRoot := filepath.Join(sourceRoot, "assets")
	if _, err := os.Stat(assetsRoot); err != nil {
		return nil
	}
	return filepath.WalkDir(assetsRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(assetsRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetRoot, "assets", relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyUpdateFile(path, target, 0o644)
	})
}

func copyUpdateFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func cleanupUpdateArtifacts(root, backup string) {
	go func() {
		for attempt := 0; attempt < 30; attempt++ {
			rootErr := error(nil)
			backupErr := error(nil)
			if root != "" {
				rootErr = os.RemoveAll(root)
			}
			if backup != "" {
				backupErr = os.Remove(backup)
			}
			if rootErr == nil && (backupErr == nil || os.IsNotExist(backupErr)) {
				if root != "" {
					parent := filepath.Dir(root)
					if filepath.Base(parent) == updateWorkspaceDir {
						_ = os.Remove(parent)
					}
				}
				return
			}
			time.Sleep(time.Second)
		}
		log.Printf("cleanup update artifacts incomplete: root=%s backup=%s", root, backup)
	}()
}

func filterUpdaterArgs(args []string) []string {
	result := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-update-helper":
			continue
		case "-update-target", "-update-package-root", "-update-restart-file", "-update-parent-pid", "-update-cleanup-root", "-update-cleanup-backup":
			index++
			continue
		default:
			result = append(result, args[index])
		}
	}
	return result
}
