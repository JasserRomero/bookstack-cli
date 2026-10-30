<#
.SYNOPSIS
Install bsk (bookstack-cli) on Windows.

.DESCRIPTION
Downloads a bsk release, verifies its SHA-256 checksum, installs bsk.exe and
adds the install directory to the user PATH.

  .\install.ps1                      install the latest release
  .\install.ps1 -Version v0.1.0      install a specific release
  .\install.ps1 -Uninstall           remove the installed binary

Environment equivalents: BSK_VERSION, BSK_INSTALL_DIR. GITHUB_TOKEN or GH_TOKEN
is used with the GitHub API when gh is unavailable.

Download strategy, in order: gh (installed and authenticated), the GitHub API
with GITHUB_TOKEN/GH_TOKEN, anonymous HTTPS (public repository only).

Test-only overrides (unset in normal use; they never change default behavior):
  BSK_INSTALL_OS         replaces the detected OS; only "windows" is accepted
  BSK_INSTALL_ARCH       replaces PROCESSOR_ARCHITECTURE (e.g. AMD64, ARM64)
  BSK_INSTALL_BASE_URL   forces anonymous downloads from
                         $BSK_INSTALL_BASE_URL/<tag>/<file>, skipping gh and tokens
  BSK_INSTALL_API_URL    GitHub API base URL (default https://api.github.com)
  BSK_INSTALL_PATH_FILE  file that stands in for the user PATH: it is read and
                         appended to instead of changing the real user PATH
#>
param(
    [string]$Version = $env:BSK_VERSION,
    [string]$InstallDir = $env:BSK_INSTALL_DIR,
    [switch]$Uninstall
)

function Install-Bsk {
    param([string]$Version, [string]$InstallDir, [switch]$Uninstall)

    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    $Repo = 'JasserRomero/bookstack-cli'
    $ApiUrl = if ($env:BSK_INSTALL_API_URL) { $env:BSK_INSTALL_API_URL } else { 'https://api.github.com' }
    $BaseUrl = $env:BSK_INSTALL_BASE_URL
    $PrivateHint = "the repository may be private: install and authenticate the GitHub CLI (gh auth login), or set GITHUB_TOKEN or GH_TOKEN to a token that can read $Repo"

    if (-not $InstallDir) {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\bsk'
    }
    $Target = Join-Path $InstallDir 'bsk.exe'

    if ($Uninstall) {
        if (Test-Path -LiteralPath $Target) {
            Remove-Item -LiteralPath $Target -Force
            Write-Host "Removed $Target"
        } else {
            Write-Host "bsk is not installed in $InstallDir"
        }
        $configBase = $env:XDG_CONFIG_HOME
        if (-not $configBase -or -not [IO.Path]::IsPathRooted($configBase)) {
            $configBase = Join-Path ([Environment]::GetFolderPath('UserProfile')) '.config'
        }
        $configDir = Join-Path $configBase 'bookstack-cli'
        Write-Host "Configuration (including credentials) was kept in $configDir; delete it manually if you no longer need it."
        return
    }

    # Platform.
    $os = $env:BSK_INSTALL_OS
    if (-not $os) {
        $os = 'windows'
        if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
            $os = [Environment]::OSVersion.Platform.ToString()
        }
    }
    if ($os -ne 'windows') {
        throw "unsupported operating system: $os (install.ps1 supports Windows; on Linux and macOS use install.sh)"
    }
    $archRaw = $env:BSK_INSTALL_ARCH
    if (-not $archRaw) { $archRaw = $env:PROCESSOR_ARCHITEW6432 }
    if (-not $archRaw) { $archRaw = $env:PROCESSOR_ARCHITECTURE }
    switch -Regex ($archRaw) {
        '^(AMD64|x86_64|amd64)$' { $arch = 'amd64'; break }
        '^(ARM64|aarch64|arm64)$' { $arch = 'arm64'; break }
        default { throw "unsupported architecture: $archRaw (supported: AMD64, ARM64)" }
    }

    if ($PSVersionTable.PSEdition -ne 'Core') {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("bsk-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        $tag = ''
        if ($Version) { $tag = 'v' + $Version.TrimStart('v') }

        $token = $env:GITHUB_TOKEN
        if (-not $token) { $token = $env:GH_TOKEN }
        $useGh = $false
        if (-not $BaseUrl -and (Get-Command gh -ErrorAction SilentlyContinue)) {
            & gh auth token *> $null
            $useGh = ($LASTEXITCODE -eq 0)
        }

        if ($useGh) {
            if (-not $tag) {
                $tag = & gh release view -R $Repo --json tagName -q .tagName
                if ($LASTEXITCODE -ne 0 -or -not $tag) { throw "gh could not find the latest release of $Repo" }
            }
            $archive = "bsk_$($tag.TrimStart('v'))_windows_$arch.zip"
            Write-Host "Downloading $archive ($tag) with gh..."
            $attempt = 1
            while ($true) {
                & gh release download $tag -R $Repo -p $archive -p checksums.txt -D $tmp --clobber
                if ($LASTEXITCODE -eq 0) { break }
                if ($attempt -ge 3) { throw "gh could not download release $tag of $Repo" }
                $attempt++
                Start-Sleep -Seconds 2
            }
        } elseif (-not $BaseUrl -and $token) {
            $headers = @{ Authorization = "Bearer $token"; Accept = 'application/vnd.github+json' }
            $releaseUrl = if ($tag) { "$ApiUrl/repos/$Repo/releases/tags/$tag" } else { "$ApiUrl/repos/$Repo/releases/latest" }
            try {
                $release = Invoke-RestMethod -Uri $releaseUrl -Headers $headers -UseBasicParsing
            } catch {
                throw "could not read release $(if ($tag) { $tag } else { 'latest' }) of $Repo with the provided token (missing release or no access)"
            }
            $tag = $release.tag_name
            $archive = "bsk_$($tag.TrimStart('v'))_windows_$arch.zip"
            Write-Host "Downloading $archive ($tag) with the GitHub API..."
            foreach ($name in @($archive, 'checksums.txt')) {
                $asset = $release.assets | Where-Object { $_.name -eq $name } | Select-Object -First 1
                if (-not $asset) { throw "release $tag has no asset named $name" }
                $assetHeaders = @{ Authorization = "Bearer $token"; Accept = 'application/octet-stream' }
                try {
                    Invoke-WebRequest -Uri $asset.url -Headers $assetHeaders -OutFile (Join-Path $tmp $name) -UseBasicParsing
                } catch {
                    throw "could not download $name from release $tag"
                }
            }
        } else {
            if (-not $tag) {
                try {
                    $tag = (Invoke-RestMethod -Uri "$ApiUrl/repos/$Repo/releases/latest" -UseBasicParsing).tag_name
                } catch {
                    throw "could not find the latest release of $Repo; $PrivateHint"
                }
                if (-not $tag) { throw "could not determine the latest release of $Repo" }
            }
            $archive = "bsk_$($tag.TrimStart('v'))_windows_$arch.zip"
            $base = if ($BaseUrl) { $BaseUrl } else { "https://github.com/$Repo/releases/download" }
            Write-Host "Downloading $archive ($tag)..."
            foreach ($name in @($archive, 'checksums.txt')) {
                try {
                    Invoke-WebRequest -Uri "$base/$tag/$name" -OutFile (Join-Path $tmp $name) -UseBasicParsing
                } catch {
                    throw "could not download $name from release $tag; $PrivateHint"
                }
            }
        }

        # Verify.
        $expected = $null
        foreach ($line in (Get-Content -LiteralPath (Join-Path $tmp 'checksums.txt'))) {
            $fields = $line.Trim() -split '\s+'
            if ($fields.Count -ge 2 -and $fields[1].TrimStart('*') -eq $archive) {
                $expected = $fields[0]
                break
            }
        }
        if (-not $expected) { throw "checksums.txt has no entry for $archive; refusing to install" }
        $actual = (Get-FileHash -LiteralPath (Join-Path $tmp $archive) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected.ToLowerInvariant()) {
            throw "checksum mismatch for $archive (expected $expected, got $actual); refusing to install"
        }
        Write-Host "Checksum verified (SHA-256 $actual)"

        # Install.
        $extract = Join-Path $tmp 'extract'
        Expand-Archive -LiteralPath (Join-Path $tmp $archive) -DestinationPath $extract -Force
        $exe = Join-Path $extract 'bsk.exe'
        if (-not (Test-Path -LiteralPath $exe)) { throw "$archive does not contain bsk.exe" }
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $staged = Join-Path $InstallDir 'bsk.exe.new'
        Copy-Item -LiteralPath $exe -Destination $staged -Force
        Move-Item -LiteralPath $staged -Destination $Target -Force
        Write-Host "Installed $Target"
        try {
            & $Target --version
        } catch {
            Write-Warning "bsk was installed but '$Target --version' could not run: $($_.Exception.Message)"
        }

        Add-UserPath -Dir $InstallDir
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Add-UserPath {
    param([string]$Dir)

    $sep = [IO.Path]::PathSeparator
    $pathFile = $env:BSK_INSTALL_PATH_FILE
    if ($pathFile) {
        $current = ''
        if (Test-Path -LiteralPath $pathFile) { $current = [IO.File]::ReadAllText($pathFile).Trim() }
    } else {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        $current = [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    }

    $want = $Dir.TrimEnd('\', '/')
    foreach ($entry in ($current -split [regex]::Escape($sep))) {
        $expanded = [Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\', '/')
        if ($expanded -and $expanded -ieq $want) {
            return
        }
    }

    $updated = if ($current) { $current.TrimEnd($sep) + $sep + $Dir } else { $Dir }
    if ($pathFile) {
        [IO.File]::WriteAllText($pathFile, $updated)
    } else {
        $key.SetValue('Path', $updated, 'ExpandString')
        $key.Close()
        # Broadcast the environment change so new terminals see the updated PATH.
        [Environment]::SetEnvironmentVariable('BSK_INSTALL_REFRESH', '1', 'User')
        [Environment]::SetEnvironmentVariable('BSK_INSTALL_REFRESH', $null, 'User')
    }
    if ($env:PATH) { $env:PATH = $env:PATH.TrimEnd($sep) + $sep + $Dir } else { $env:PATH = $Dir }
    Write-Host "Added $Dir to your user PATH; restart your terminal to use bsk."
}

Install-Bsk -Version $Version -InstallDir $InstallDir -Uninstall:$Uninstall
