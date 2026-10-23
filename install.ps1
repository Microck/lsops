[CmdletBinding()]
param(
    [string]$InstallDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'Use install.sh on Linux or macOS.'
}

$nativeArchitecture = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrWhiteSpace($nativeArchitecture)) {
    $nativeArchitecture = $env:PROCESSOR_ARCHITECTURE
}
switch ($nativeArchitecture) {
    'AMD64' { $targetArchitecture = 'amd64' }
    'ARM64' { $targetArchitecture = 'arm64' }
    default { throw 'Supported Windows architectures are amd64 and arm64 (arm64 requires a local Go build).' }
}

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    $localAppData = $env:LOCALAPPDATA
    if ([string]::IsNullOrWhiteSpace($localAppData)) {
        $localAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    }
    if ([string]::IsNullOrWhiteSpace($localAppData)) {
        throw 'Cannot locate LOCALAPPDATA; specify -InstallDir.'
    }
    $InstallDir = Join-Path $localAppData 'lsops\bin'
}
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$sourceBinary = Join-Path $PSScriptRoot "bin\windows-$targetArchitecture\lsops.exe"
$buildFromSource = -not (Test-Path -LiteralPath $sourceBinary -PathType Leaf)
$goCommand = $null
if ($buildFromSource) {
    $goCommand = Get-Command go -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $goCommand) {
        throw "No bundled binary for windows-$targetArchitecture. Install Go 1.23 or later to build the included source."
    }
    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'go.mod') -PathType Leaf)) {
        throw 'Included Go source is missing.'
    }
}

[IO.Directory]::CreateDirectory($InstallDir) | Out-Null
$destination = Join-Path $InstallDir 'lsops.exe'
if (Test-Path -LiteralPath $destination) {
    $existing = Get-Item -LiteralPath $destination -Force
    if ($existing.PSIsContainer -or ($existing.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw "Refusing to replace a directory or link: $destination"
    }
}
$temporaryFile = Join-Path $InstallDir ('.lsops-' + [Guid]::NewGuid().ToString('N') + '.tmp')

try {
    if ($buildFromSource) {
        Write-Output "No bundled binary for windows-$targetArchitecture; building included source locally."
        $buildEnvironment = @{
            GOOS = 'windows'
            GOARCH = $targetArchitecture
            CGO_ENABLED = '0'
            GOTOOLCHAIN = 'local'
            GOWORK = 'off'
            GOPROXY = 'off'
            GOSUMDB = 'off'
        }
        $previousEnvironment = @{}
        foreach ($name in $buildEnvironment.Keys) {
            $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        }
        Push-Location -LiteralPath $PSScriptRoot
        try {
            foreach ($name in $buildEnvironment.Keys) {
                [Environment]::SetEnvironmentVariable($name, $buildEnvironment[$name], 'Process')
            }
            & $goCommand.Source build -trimpath -buildvcs=false -mod=readonly -ldflags '-s -w' -o $temporaryFile .
            if ($LASTEXITCODE -ne 0) {
                throw "Go build failed with exit code $LASTEXITCODE."
            }
        }
        finally {
            foreach ($name in $buildEnvironment.Keys) {
                [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
            }
            Pop-Location
        }
    }
    else {
        [IO.File]::Copy($sourceBinary, $temporaryFile, $false)
    }

    if (Test-Path -LiteralPath $destination) {
        $existing = Get-Item -LiteralPath $destination -Force
        if ($existing.PSIsContainer -or ($existing.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw "Refusing to replace a directory or link: $destination"
        }
        [IO.File]::Replace($temporaryFile, $destination, $null)
    }
    else {
        [IO.File]::Move($temporaryFile, $destination)
    }
}
finally {
    if (Test-Path -LiteralPath $temporaryFile) {
        Remove-Item -LiteralPath $temporaryFile -Force -ErrorAction SilentlyContinue
    }
}

Write-Output "Installed lsops: $destination"
$pathEntries = @($env:Path -split ';' | ForEach-Object { $_.Trim().Trim('"').TrimEnd([char[]]'\/') })
if ($pathEntries -notcontains $InstallDir.TrimEnd([char[]]'\/')) {
    Write-Output 'This directory is not on PATH. Add it to your user PATH in Windows Environment Variables, then restart your terminal or agent; or use the full executable path above.'
}
