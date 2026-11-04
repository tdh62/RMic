//go:build windows

package virtual

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	windowsdriver "cnb.cool/tdh6/RMic/drivers/windows"
)

func windowsCableStatusFor(driver string) (string, error) {
	name := map[string]string{"vbcable": "VB-Audio Virtual Cable", "vbcable-a": "VB-Audio Cable A", "vbcable-b": "VB-Audio Cable B"}[driver]
	if name == "" {
		return "", fmt.Errorf("unknown VB-CABLE driver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	friendly := name
	if driver == "vbcable-a" {
		friendly = "VB-Audio CABLE-A"
	}
	if driver == "vbcable-b" {
		friendly = "VB-Audio CABLE-B"
	}
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; [Console]::OutputEncoding=New-Object System.Text.UTF8Encoding; Get-CimInstance Win32_SoundDevice | Where-Object { @('%s','%s') -contains $_.Name } | Select-Object -ExpandProperty Name`, name, friendly)
	data, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("查询 Windows 声卡失败：%w；%s", err, strings.TrimSpace(string(data)))
	}
	return strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff")), nil
}

const installerScript = `param([string]$Setup, [string]$Action, [string]$Mode)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding
try {
    if ($Action -eq 'install') { $arguments = @('-i', '-h') }
    elseif ($Action -eq 'uninstall') { $arguments = @('-u', '-h') }
    else { throw 'Invalid driver action' }
    if ($Mode -eq 'interactive') {
        $process = Start-Process -FilePath $Setup -Verb RunAs -Wait -PassThru
    } else {
        $process = Start-Process -FilePath $Setup -ArgumentList $arguments -Verb RunAs -Wait -PassThru
    }
    exit $process.ExitCode
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1602
}
`

func manageWindows(action, dir string) (string, error) {
	return manageWindowsCable(action, dir, "vbcable")
}
func manageWindowsCable(action, dir, driver string) (string, error) {
	if driver != "vbcable" {
		return "", fmt.Errorf("公开分发版不提供 CABLE-A/B 驱动管理")
	}
	_, configErr := windowsdriver.LocalArchive(dir)
	if action != "status" && configErr != nil {
		return "", fmt.Errorf("%w；请从 https://vb-audio.com/Cable/ 手动下载安装", configErr)
	}
	status, err := windowsCableStatusFor(driver)
	if err != nil {
		return "", err
	}
	if action == "status" {
		if status == "" {
			status = "未检测到 VB-CABLE；安装后请重启并刷新状态。"
		} else {
			status = "检测到：" + status
		}
		guide := "请从 https://vb-audio.com/Cable/ 手动下载安装。"
		if configErr == nil {
			guide = "已启用本地原版 VB-CABLE 安装包；安装和卸载需确认系统 UAC。"
		}
		return status + "\n" + guide + "\n" + PlatformGuide(), nil
	}
	if action == "install" && status != "" {
		return "已检测到 VB-CABLE，无需重复安装。", nil
	}
	if action == "uninstall" && status == "" {
		return "未检测到 VB-CABLE；如果刚执行卸载，请重启电脑完成清理。", nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defaults, err := captureAudioDefaults()
	if err != nil {
		return "", fmt.Errorf("无法记录默认音频设备，未执行驱动操作：%w", err)
	}
	defer defaults.close()
	folder, err := windowsdriver.Prepare(dir)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	// An elevated installer can outlive its PowerShell parent on timeout. Keep
	// its input files in that case instead of deleting files still in use.
	defer func() {
		if ctx.Err() == nil {
			_ = os.RemoveAll(folder)
		}
	}()
	script := filepath.Join(folder, "rmic-driver.ps1")
	if err = os.WriteFile(script, []byte(installerScript), 0600); err != nil {
		return "", err
	}
	setup := filepath.Join(folder, "VBCABLE_Setup_x64.exe")
	installerMode := "automatic"
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, setup, action, installerMode)
	output, runErr := command.CombinedOutput()
	restoreErr := defaults.restore()
	if ctx.Err() != nil {
		return "", errors.Join(fmt.Errorf("驱动操作超时，安装器可能仍在运行；文件保留于 %s：%w", folder, ctx.Err()), restoreErr)
	}
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return "", errors.Join(runErr, restoreErr)
		}
		code = exit.ExitCode()
		if code != 1 && code != 2 && code != 3010 && code != 1641 {
			return "", errors.Join(fmt.Errorf("驱动操作失败或授权已取消（退出码 %d）：%s", code, strings.TrimSpace(string(output))), restoreErr)
		}
	}
	after, queryErr := windowsCableStatusFor(driver)
	result := fmt.Sprintf("官方驱动%s流程已结束（退出码 %d）。请重启电脑完成操作。", map[string]string{"install": "安装", "uninstall": "卸载"}[action], code)
	if queryErr != nil {
		result += "\n暂时无法确认设备状态：" + queryErr.Error()
	} else if action == "install" && after == "" || action == "uninstall" && after != "" {
		result += "\n设备状态尚未完成切换，重启后请按 r 再次核实。"
	}
	if restoreErr != nil {
		result += "\n请检查系统默认音频设备：" + restoreErr.Error()
	}
	return result, nil
}
