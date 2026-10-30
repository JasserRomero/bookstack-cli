# Tests for install.ps1, run by test/install/run.sh inside the
# mcr.microsoft.com/powershell image. install.ps1 is mounted at /src/install.ps1.
# Uses the test-only overrides (BSK_INSTALL_OS/ARCH/BASE_URL/API_URL/PATH_FILE)
# to fetch the Windows zip on Linux and record PATH changes in a file.
#
# Environment: BSK_TEST_BASE_URL, BSK_TEST_API_URL (fake release server),
# BSK_TEST_REAL_VERSION and GH_TOKEN (optional real-release check).
$ErrorActionPreference = 'Stop'
$installer = '/src/install.ps1'
$script:fails = 0
$work = Join-Path ([IO.Path]::GetTempPath()) ("bsk-ps-test-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
$realToken = $env:GH_TOKEN
$env:GH_TOKEN = $null
$env:GITHUB_TOKEN = $null

function Check([string]$Name, [bool]$Ok) {
    if ($Ok) { Write-Output "PASS $Name" } else { Write-Output "FAIL $Name"; $script:fails++ }
}

$script:n = 0
# Invoke-Installer runs install.ps1 in a child pwsh with the fake-server
# overrides plus $Vars, a fresh TMPDIR and HOME; returns Out, Rc, Tmp, PathFile.
# With -ViaIex the script text is piped to Invoke-Expression, as `irm ... | iex` does.
# With -NativeErrorsTerminate the child sets $PSNativeCommandUseErrorActionPreference,
# so a failing native command is a terminating error under 'Stop', as in
# Windows PowerShell 5.1 when a native command's stderr is redirected.
function Invoke-Installer([hashtable]$Vars, [string[]]$Arguments, [switch]$ViaIex, [switch]$NativeErrorsTerminate) {
    $script:n++
    $tmp = Join-Path $work "tmp$script:n"
    New-Item -ItemType Directory -Path $tmp | Out-Null
    $all = @{
        BSK_INSTALL_OS        = 'windows'
        BSK_INSTALL_ARCH      = 'AMD64'
        BSK_INSTALL_BASE_URL  = $env:BSK_TEST_BASE_URL
        BSK_INSTALL_API_URL   = $env:BSK_TEST_API_URL
        BSK_INSTALL_PATH_FILE = Join-Path $work 'userpath.txt'
        BSK_VERSION           = $null
        BSK_INSTALL_DIR       = $null
        TMPDIR                = $tmp
        HOME                  = Join-Path $work "home$script:n"
    }
    foreach ($k in $Vars.Keys) { $all[$k] = $Vars[$k] }
    $saved = @{}
    foreach ($k in $all.Keys) {
        $saved[$k] = [Environment]::GetEnvironmentVariable($k)
        [Environment]::SetEnvironmentVariable($k, $all[$k])
    }
    try {
        $ErrorActionPreference = 'Continue'
        if ($NativeErrorsTerminate) {
            $out = & pwsh -NoProfile -Command "`$PSNativeCommandUseErrorActionPreference = `$true; & '$installer'" 2>&1 | Out-String
        } elseif ($ViaIex) {
            $out = & pwsh -NoProfile -Command "Get-Content -Raw '$installer' | Invoke-Expression" 2>&1 | Out-String
        } else {
            $out = & pwsh -NoProfile -File $installer @Arguments 2>&1 | Out-String
        }
        $rc = $LASTEXITCODE
    } finally {
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
    }
    [pscustomobject]@{ Out = $out; Rc = $rc; Tmp = $tmp; PathFile = $all.BSK_INSTALL_PATH_FILE }
}

function Read-Text([string]$Path) { [IO.File]::ReadAllText($Path) }
function Test-Empty([string]$Dir) { @(Get-ChildItem -LiteralPath $Dir -Force).Count -eq 0 }

# --- static checks ------------------------------------------------------------

$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile($installer, [ref]$tokens, [ref]$errors) | Out-Null
Check 'ps1: parses with zero errors' ($errors.Count -eq 0)
$errors | ForEach-Object { Write-Output "  $($_.Extent.StartLineNumber): $($_.Message)" }

$analyzer = $false
try {
    # 1.22.0 is the newest release that supports the image's PowerShell 7.4.2.
    Install-Module PSScriptAnalyzer -RequiredVersion 1.22.0 -Scope CurrentUser -Force -ErrorAction Stop *> $null
    Import-Module PSScriptAnalyzer -ErrorAction Stop
    $analyzer = $true
} catch {
    Write-Output "SKIP ps1: PSScriptAnalyzer not installable ($($_.Exception.Message))"
}
if ($analyzer) {
    $findings = @(Invoke-ScriptAnalyzer -Path $installer -Severity Error)
    Check 'ps1: PSScriptAnalyzer reports no errors' ($findings.Count -eq 0)
    $findings | ForEach-Object { Write-Output "  $($_.Line): $($_.RuleName) $($_.Message)" }
    $compat = @(Invoke-ScriptAnalyzer -Path $installer -Settings @{
            IncludeRules = @('PSUseCompatibleSyntax')
            Rules        = @{ PSUseCompatibleSyntax = @{ Enable = $true; TargetVersions = @('5.1', '7.0') } }
        })
    Check 'ps1: syntax compatible with PowerShell 5.1 and 7' ($compat.Count -eq 0)
    $compat | ForEach-Object { Write-Output "  $($_.Line): $($_.Message)" }
}

# --- against the fake server --------------------------------------------------

$dir = Join-Path $work 'i1'
$r = Invoke-Installer @{} @('-Version', '9.9.9', '-InstallDir', $dir)
Check 'ps1: success exits 0' ($r.Rc -eq 0)
Check 'ps1: downloads windows_amd64 zip' ($r.Out -like '*bsk_9.9.9_windows_amd64.zip (v9.9.9)*')
Check 'ps1: checksum verified' ($r.Out -like '*Checksum verified*')
Check 'ps1: bsk.exe installed from the archive' ((Read-Text (Join-Path $dir 'bsk.exe')) -eq "fake bsk.exe 9.9.9`n")
Check 'ps1: only bsk.exe in install dir' ((@(Get-ChildItem -LiteralPath $dir -Force).Name -join ',') -eq 'bsk.exe')
Check 'ps1: install dir added to user PATH' ((Read-Text $r.PathFile) -eq $dir)
Check 'ps1: PATH change reported' ($r.Out -like "*Added $dir to your user PATH*")
Check 'ps1: temp dir removed' (Test-Empty $r.Tmp)
if ($r.Rc -ne 0) { Write-Output $r.Out }

Set-Content -LiteralPath (Join-Path $dir 'bsk.exe') -Value 'old' -NoNewline
$r = Invoke-Installer @{ BSK_INSTALL_DIR = $dir } @()
Check 'ps1: latest via API and BSK_INSTALL_DIR exits 0' ($r.Rc -eq 0)
Check 'ps1: latest release resolved' ($r.Out -like '*(v9.9.9)*')
Check 'ps1: existing binary replaced' ((Read-Text (Join-Path $dir 'bsk.exe')) -eq "fake bsk.exe 9.9.9`n")
Check 'ps1: no staged file left' ((@(Get-ChildItem -LiteralPath $dir -Force).Name -join ',') -eq 'bsk.exe')
Check 'ps1: PATH entry not duplicated' ((Read-Text $r.PathFile) -eq $dir)
Check 'ps1: no PATH change reported when present' (-not ($r.Out -like '*to your user PATH*'))

$r = Invoke-Installer @{ BSK_VERSION = 'v9.9.9'; BSK_INSTALL_ARCH = 'ARM64' } @('-InstallDir', (Join-Path $work 'i2'))
Check 'ps1: ARM64 maps to windows_arm64' (($r.Rc -eq 0) -and ($r.Out -like '*bsk_9.9.9_windows_arm64.zip*'))

# R4-001: gh installed but not logged in must fall back to the token, even
# where native command failures terminate the script (Windows PowerShell 5.1).
$fakeBin = Join-Path $work 'fakebin'
New-Item -ItemType Directory -Path $fakeBin | Out-Null
$fakeGh = Join-Path $fakeBin 'gh'
Set-Content -LiteralPath $fakeGh -Value "#!/bin/sh`necho 'no oauth token found' >&2`nexit 1`n" -NoNewline
& chmod +x $fakeGh
$dir = Join-Path $work 'nogh'
$r = Invoke-Installer @{ BSK_INSTALL_BASE_URL = $null; GITHUB_TOKEN = 'fake-token'; BSK_INSTALL_DIR = $dir; PATH = "$fakeBin$([IO.Path]::PathSeparator)$env:PATH" } @() -NativeErrorsTerminate
Check 'ps1: gh without login falls back to the token' (($r.Rc -eq 0) -and ($r.Out -like '*with the GitHub API...*') -and (Test-Path -LiteralPath (Join-Path $dir 'bsk.exe')))
if ($r.Rc -ne 0) { Write-Output $r.Out }

$dir = Join-Path $work 'iex'
$r = Invoke-Installer @{ BSK_VERSION = '9.9.9'; BSK_INSTALL_DIR = $dir } @() -ViaIex
Check 'ps1: runs through Invoke-Expression (irm | iex)' (($r.Rc -eq 0) -and ($r.Out -like '*Checksum verified*') -and (Test-Path -LiteralPath (Join-Path $dir 'bsk.exe')))
if ($r.Rc -ne 0) { Write-Output $r.Out }
$r = Invoke-Installer @{ BSK_VERSION = 'v9.9.8'; BSK_INSTALL_DIR = (Join-Path $work 'iex2') } @() -ViaIex
Check 'ps1: Invoke-Expression failure reports the error' (($r.Rc -ne 0) -and ($r.Out -like '*checksum mismatch*'))

$dir = Join-Path $work 'i3'
New-Item -ItemType Directory -Path $dir | Out-Null
Set-Content -LiteralPath (Join-Path $dir 'bsk.exe') -Value 'old' -NoNewline
$r = Invoke-Installer @{} @('-Version', 'v9.9.8', '-InstallDir', $dir)
Check 'ps1: checksum mismatch exits non-zero' ($r.Rc -ne 0)
Check 'ps1: checksum mismatch message' ($r.Out -like '*checksum mismatch for bsk_9.9.8_windows_amd64.zip*')
Check 'ps1: checksum mismatch keeps existing binary' ((Read-Text (Join-Path $dir 'bsk.exe')) -eq 'old')
Check 'ps1: checksum mismatch leaves no staged file' ((@(Get-ChildItem -LiteralPath $dir -Force).Name -join ',') -eq 'bsk.exe')
Check 'ps1: temp dir removed on failure' (Test-Empty $r.Tmp)

$dir = Join-Path $work 'i4'
$r = Invoke-Installer @{} @('-Version', 'v9.9.7', '-InstallDir', $dir)
Check 'ps1: missing checksum entry exits non-zero' ($r.Rc -ne 0)
Check 'ps1: missing checksum entry message' ($r.Out -like '*checksums.txt has no entry for bsk_9.9.7_windows_amd64.zip*')
Check 'ps1: missing checksum entry installs nothing' (-not (Test-Path -LiteralPath $dir))

$r = Invoke-Installer @{ BSK_INSTALL_ARCH = 'x86' } @('-Version', 'v9.9.9', '-InstallDir', (Join-Path $work 'i5'))
Check 'ps1: unsupported arch exits non-zero' ($r.Rc -ne 0)
Check 'ps1: unsupported arch message' ($r.Out -like '*unsupported architecture: x86*')
$r = Invoke-Installer @{ BSK_INSTALL_OS = $null } @('-Version', 'v9.9.9', '-InstallDir', (Join-Path $work 'i5'))
Check 'ps1: non-Windows OS exits non-zero' ($r.Rc -ne 0)
Check 'ps1: non-Windows OS message' ($r.Out -like '*unsupported operating system*')
Check 'ps1: unsupported platform installs nothing' (-not (Test-Path -LiteralPath (Join-Path $work 'i5')))

$dir = Join-Path $work 'i6'
New-Item -ItemType Directory -Path $dir | Out-Null
Set-Content -LiteralPath (Join-Path $dir 'bsk.exe') -Value 'old'
Set-Content -LiteralPath (Join-Path $dir 'other') -Value 'keep'
$cfgHome = Join-Path $work 'cfg-home'
$cfg = Join-Path $cfgHome '.config/bookstack-cli/config.toml'
New-Item -ItemType Directory -Path (Split-Path $cfg) -Force | Out-Null
Set-Content -LiteralPath $cfg -Value 'token = "x"'
$r = Invoke-Installer @{ HOME = $cfgHome } @('-Uninstall', '-InstallDir', $dir)
Check 'ps1: uninstall exits 0' ($r.Rc -eq 0)
Check 'ps1: uninstall removes bsk.exe' (-not (Test-Path -LiteralPath (Join-Path $dir 'bsk.exe')))
Check 'ps1: uninstall keeps other files' (Test-Path -LiteralPath (Join-Path $dir 'other'))
Check 'ps1: uninstall keeps config' (Test-Path -LiteralPath $cfg)
Check 'ps1: uninstall names config dir' ($r.Out -like "*kept in $(Join-Path $cfgHome '.config/bookstack-cli')*")
$r = Invoke-Installer @{} @('-Uninstall', '-InstallDir', $dir)
Check 'ps1: uninstall when absent' (($r.Rc -eq 0) -and ($r.Out -like "*bsk is not installed in $dir*"))

# --- against the real release (token path) ------------------------------------

if ($realToken -and $env:BSK_TEST_REAL_VERSION) {
    $dir = Join-Path $work 'real'
    $r = Invoke-Installer @{ BSK_INSTALL_BASE_URL = $null; BSK_INSTALL_API_URL = $null; GH_TOKEN = $realToken } @('-Version', "v$($env:BSK_TEST_REAL_VERSION)", '-InstallDir', $dir)
    Check 'ps1 real token: exits 0' ($r.Rc -eq 0)
    Check 'ps1 real token: used the GitHub API' ($r.Out -like '*with the GitHub API...*')
    Check 'ps1 real token: checksum verified' ($r.Out -like '*Checksum verified*')
    $exe = Join-Path $dir 'bsk.exe'
    $isPe = $false
    if (Test-Path -LiteralPath $exe) {
        $bytes = [IO.File]::ReadAllBytes($exe)
        $isPe = ($bytes.Length -gt 1MB) -and ($bytes[0] -eq 0x4D) -and ($bytes[1] -eq 0x5A)
    }
    Check 'ps1 real token: bsk.exe is a Windows executable' $isPe
    Check 'ps1 real token: token never printed' (-not $r.Out.Contains($realToken))
    if ($r.Rc -ne 0) { Write-Output $r.Out.Replace($realToken, '***') }
} else {
    Write-Output 'SKIP ps1 real token: GH_TOKEN or BSK_TEST_REAL_VERSION not set'
}

Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
exit $script:fails
