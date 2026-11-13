# SPDX-License-Identifier: MPL-2.0
# Install the public Lefalock release for the current Windows user.
#Requires -Version 5.1
param(
    [ValidatePattern('^v[0-9]+\.[0-9]+\.[0-9]+$')]
    [string]$Version = 'v0.0.15'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') {
    throw 'Download install.ps1 from a release, or specify -Version vX.Y.Z.'
}
if ($env:OS -ne 'Windows_NT') { throw 'This installer supports Windows.' }
$architecture = $env:PROCESSOR_ARCHITEW6432
if (-not $architecture) { $architecture = $env:PROCESSOR_ARCHITECTURE }
if ($architecture -ne 'AMD64') { throw 'This release supports Windows amd64.' }
if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set.' }

# Windows PowerShell 5.1 may otherwise negotiate an obsolete TLS version.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$temporary = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
$installDirectory = Join-Path $env:LOCALAPPDATA 'Programs\Lefalock'
$staged = $null
New-Item -ItemType Directory -Path $temporary | Out-Null
function Download-ReleaseFile([string]$Uri, [string]$Destination) {
    # Follow HTTPS redirects explicitly, including GitHub's asset CDN.
    for ($redirect = 0; $redirect -le 5; $redirect++) {
        $url = [Uri]$Uri
        if ($url.Scheme -ne 'https') { throw 'Downloads must use HTTPS.' }
        $request = [Net.HttpWebRequest]::Create($url)
        $request.AllowAutoRedirect = $false
        $request.Timeout = 600000
        $request.ReadWriteTimeout = 600000
        $request.UserAgent = 'Lefalock-Installer'
        $response = $request.GetResponse()
        try {
            $status = [int]$response.StatusCode
            if ($status -ge 300 -and $status -lt 400) {
                $location = $response.Headers['Location']
                if (-not $location) { throw 'Redirect has no location.' }
                $Uri = ([Uri]::new($url, $location)).AbsoluteUri
                continue
            }
            if ($status -ne 200) { throw "Download failed: HTTP $status" }
            $inputStream = $response.GetResponseStream()
            $outputStream = [IO.File]::Create($Destination)
            try {
                $buffer = New-Object byte[] 65536
                $total = 0L
                while (($count = $inputStream.Read($buffer, 0, $buffer.Length)) -gt 0) {
                    $total += $count
                    if ($total -gt 134217728) { throw 'Download is too large.' }
                    $outputStream.Write($buffer, 0, $count)
                }
            } finally {
                $inputStream.Dispose()
                $outputStream.Dispose()
            }
            return
        } finally { $response.Dispose() }
    }
    throw 'Too many download redirects.'
}
try {
    $archive = "lefalock_${Version}_windows_amd64.zip"
    $base = "https://github.com/Lefalock/clients-cli/releases/download/$Version"
    $manifestPath = Join-Path $temporary 'SHA256SUMS'
    $archivePath = Join-Path $temporary $archive
    Write-Host "Downloading Lefalock $Version (windows/amd64)..."
    Download-ReleaseFile "$base/SHA256SUMS" $manifestPath
    Download-ReleaseFile "$base/$archive" $archivePath
    $pattern = '^([0-9a-fA-F]{64})  ' + [regex]::Escape($archive) + '$'
    $checksums = @(Get-Content -LiteralPath $manifestPath | Where-Object {
        ($_ -split '\s+')[-1] -eq $archive
    })
    if ($checksums.Count -ne 1 -or $checksums[0] -notmatch $pattern) {
        throw 'Missing, duplicate or invalid archive checksum.'
    }
    $expected = [regex]::Match($checksums[0], $pattern).Groups[1].Value
    $actual = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash
    if ($actual -ne $expected) { throw 'SHA256 mismatch. Nothing was installed.' }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $names = @($zip.Entries | ForEach-Object { $_.FullName })
        if ($names.Count -ne 2 -or
            @($names | Where-Object { $_ -eq 'lefalock.exe' }).Count -ne 1 -or
            @($names | Where-Object { $_ -eq 'README.md' }).Count -ne 1) {
            throw 'Unexpected release archive contents.'
        }
        foreach ($entry in $zip.Entries) {
            # Reject symlinks and other special Unix file types in ZIP entries.
            $kind = ($entry.ExternalAttributes -shr 16) -band 0xF000
            if ($kind -ne 0 -and $kind -ne 0x8000) {
                throw 'The release archive must contain only regular files.'
            }
        }
    } finally { $zip.Dispose() }
    $extracted = Join-Path $temporary 'extracted'
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extracted

    New-Item -ItemType Directory -Path $installDirectory -Force | Out-Null
    foreach ($path in @($installDirectory,
        (Join-Path $installDirectory 'lefalock.exe'),
        (Join-Path $installDirectory 'README.md'))) {
        if (Test-Path -LiteralPath $path) {
            $item = Get-Item -LiteralPath $path -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw 'Installation paths must not be symlinks or junctions.'
            }
        }
    }
    $staged = Join-Path $installDirectory ([Guid]::NewGuid().ToString() + '.exe')
    Copy-Item -LiteralPath (Join-Path $extracted 'lefalock.exe') -Destination $staged
    $destination = Join-Path $installDirectory 'lefalock.exe'
    if (Test-Path -LiteralPath $destination) {
        # Atomic replacement; a running/locked executable leaves the old file intact.
        [IO.File]::Replace($staged, $destination, [NullString]::Value)
    } else {
        [IO.File]::Move($staged, $destination)
    }
    $staged = $null
    Copy-Item -LiteralPath (Join-Path $extracted 'README.md') `
        -Destination (Join-Path $installDirectory 'README.md') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @($userPath -split ';' | Where-Object { $_ })
    if ($entries -notcontains $installDirectory) {
        $newPath = (@($installDirectory) + $entries) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    }
    if (($env:Path -split ';') -notcontains $installDirectory) {
        $env:Path = "$installDirectory;$env:Path"
    }
    Write-Host "Installed Lefalock $Version in $installDirectory"
    Write-Host 'Open a new terminal, then run: lefalock'
} finally {
    if ($staged -and (Test-Path -LiteralPath $staged)) {
        Remove-Item -LiteralPath $staged -Force
    }
    Remove-Item -LiteralPath $temporary -Recurse -Force
}
