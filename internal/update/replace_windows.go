// SPDX-License-Identifier: MPL-2.0

//go:build windows

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func RunPrivilegedInstall(args []string) (bool, error) {
	if len(args) > 0 && args[0] == "__install-verified-update" {
		return true, errors.New("privileged update installer is not used on Windows")
	}
	return false, nil
}

func replaceExecutable(source, target, statusPath string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(statusPath), 0o700); err != nil {
		return false, fmt.Errorf("create Windows update status directory: %w", err)
	}
	if err := os.WriteFile(statusPath, []byte("pending\n"), 0o600); err != nil {
		return false, fmt.Errorf("write Windows update status: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".lefalock-update-*.exe")
	if err != nil {
		os.Remove(statusPath)
		return false, err
	}
	temporaryPath := temporary.Name()
	sourceFile, err := os.Open(source)
	if err != nil {
		temporary.Close()
		os.Remove(temporaryPath)
		os.Remove(statusPath)
		return false, err
	}
	_, copyErr := io.Copy(temporary, sourceFile)
	closeSourceErr := sourceFile.Close()
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil || closeSourceErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(temporaryPath)
		os.Remove(statusPath)
		return false, fmt.Errorf("stage Windows update: copy=%v source-close=%v sync=%v close=%v", copyErr, closeSourceErr, syncErr, closeErr)
	}
	script := `
function Write-UpdateStatus([string]$Value) {
  $temporaryStatus = $env:LEFALOCK_UPDATE_STATUS + ".tmp"
  [System.IO.File]::WriteAllText($temporaryStatus, $Value)
  Move-Item -LiteralPath $temporaryStatus -Destination $env:LEFALOCK_UPDATE_STATUS -Force
}
$process = Get-Process -Id ([int]$env:LEFALOCK_UPDATE_PID) -ErrorAction SilentlyContinue
if ($process) { $process.WaitForExit() }
$lastError = "unknown replacement error"
for ($attempt = 0; $attempt -lt 20; $attempt++) {
  try {
    [System.IO.File]::Replace($env:LEFALOCK_UPDATE_SOURCE, $env:LEFALOCK_UPDATE_TARGET, $null, $true)
    Write-UpdateStatus "ok"
    exit 0
  } catch {
    $lastError = $_.Exception.Message
    Start-Sleep -Milliseconds 250
  }
}
Remove-Item -LiteralPath $env:LEFALOCK_UPDATE_SOURCE -Force -ErrorAction SilentlyContinue
Write-UpdateStatus ("error: " + $lastError)
exit 1
`
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	command.Env = append(os.Environ(),
		"LEFALOCK_UPDATE_PID="+strconv.Itoa(os.Getpid()),
		"LEFALOCK_UPDATE_SOURCE="+temporaryPath,
		"LEFALOCK_UPDATE_TARGET="+target,
		"LEFALOCK_UPDATE_STATUS="+statusPath,
	)
	if err := command.Start(); err != nil {
		os.Remove(temporaryPath)
		os.Remove(statusPath)
		return false, fmt.Errorf("start Windows update installer: %w", err)
	}
	_ = command.Process.Release()
	return true, nil
}
