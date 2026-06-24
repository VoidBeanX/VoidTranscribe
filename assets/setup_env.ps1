param(
    [string]$InstallDir = (Split-Path -Parent $MyInvocation.MyCommand.Path)
)

$ErrorActionPreference = "Stop"
Set-Location $InstallDir

Write-Host "=== VoidTranscribe Dependency Provisioner ===" -ForegroundColor Cyan
Write-Host "[PROGRESS] 2 | Setting up cache directories..."

# 1. Setup cache/engine directory
if (-not (Test-Path "cache\engine")) {
    New-Item -ItemType Directory -Path "cache\engine" -Force | Out-Null
    Write-Host "[+] Created 'cache/engine' directory" -ForegroundColor Green
}

# 2. Sanity Check / Install Portable Python 3.10.11
$PythonExe = "cache\engine\python.exe"
$PythonValid = $false
if (Test-Path $PythonExe) {
    # Check if python runs successfully and returns 0
    $TestRun = Start-Process -FilePath $PythonExe -ArgumentList "-c ""print('OK')""" -Wait -NoNewWindow -PassThru -ErrorAction SilentlyContinue
    if ($TestRun -and $TestRun.ExitCode -eq 0) {
        $PythonValid = $true
    }
}

if (-not $PythonValid) {
    Write-Host "[PROGRESS] 5 | Downloading Python 3.10.11 embeddable (amd64)..." -ForegroundColor Yellow
    
    # If python.exe exists but is invalid, wipe the directory to ensure clean slate
    if (Test-Path "cache\engine") {
        Remove-Item "cache\engine" -Recurse -Force -ErrorAction SilentlyContinue
        New-Item -ItemType Directory -Path "cache\engine" -Force | Out-Null
    }
    
    $PythonZipUrl = "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip"
    $PythonZipFile = "cache\python_embed.zip"

    Invoke-WebRequest -Uri $PythonZipUrl -OutFile $PythonZipFile
    Write-Host "[PROGRESS] 12 | Extracting Python to cache/engine/..." -ForegroundColor Yellow
    Expand-Archive -Path $PythonZipFile -DestinationPath "cache\engine" -Force
    Remove-Item $PythonZipFile -Force
    Write-Host "[+] Python embeddable extracted successfully" -ForegroundColor Green
} else {
    Write-Host "[+] Python embeddable already present and valid in cache/engine/" -ForegroundColor Gray
}

# 3. Configure python310._pth (uncomment import site)
Write-Host "[PROGRESS] 15 | Configuring python310._pth..."
$PthFile = "cache\engine\python310._pth"
if (Test-Path $PthFile) {
    $PthContent = Get-Content $PthFile
    $UpdatedContent = @()
    $Modified = $false

    foreach ($Line in $PthContent) {
        if ($Line.Trim() -eq "#import site" -or $Line.Trim() -eq "# import site") {
            $UpdatedContent += "import site"
            $Modified = $true
        } else {
            $UpdatedContent += $Line
        }
    }

    if ($Modified) {
        $UpdatedContent | Set-Content $PthFile
        Write-Host "[+] Configured import site in python310._pth" -ForegroundColor Green
    } else {
        Write-Host "[+] python310._pth already configured" -ForegroundColor Gray
    }
}

# 4. Sanity Check / Install Pip
$PipInstalled = $false
if (Test-Path "cache\engine\Scripts\pip.exe") {
    $TestPip = Start-Process -FilePath "cache\engine\python.exe" -ArgumentList "-m pip --version" -Wait -NoNewWindow -PassThru -ErrorAction SilentlyContinue
    if ($TestPip -and $TestPip.ExitCode -eq 0) {
        $PipInstalled = $true
    }
}

if (-not $PipInstalled) {
    Write-Host "[PROGRESS] 18 | Downloading pip installer..." -ForegroundColor Yellow
    $GetPipUrl = "https://bootstrap.pypa.io/get-pip.py"
    $GetPipFile = "cache\get-pip.py"

    Invoke-WebRequest -Uri $GetPipUrl -OutFile $GetPipFile
    Write-Host "[PROGRESS] 20 | Installing pip..." -ForegroundColor Yellow
    # Execute pip setup
    Start-Process -FilePath ".\cache\engine\python.exe" -ArgumentList $GetPipFile -Wait -NoNewWindow
    Remove-Item $GetPipFile -Force
    Write-Host "[+] Pip installed successfully" -ForegroundColor Green
} else {
    Write-Host "[+] Pip already installed and valid" -ForegroundColor Gray
}

# 5. Sanity Check / Install faster-whisper inside site-packages
$SitePackagesDir = "cache\engine\Lib\site-packages"
$FasterWhisperDir = Join-Path $SitePackagesDir "faster_whisper"
$FasterWhisperValid = $false
if (Test-Path $FasterWhisperDir) {
    $TestWhisper = Start-Process -FilePath "cache\engine\python.exe" -ArgumentList "-c ""import faster_whisper; print('OK')""" -Wait -NoNewWindow -PassThru -ErrorAction SilentlyContinue
    if ($TestWhisper -and $TestWhisper.ExitCode -eq 0) {
        $FasterWhisperValid = $true
    }
}

if (-not $FasterWhisperValid) {
    Write-Host "[PROGRESS] 32 | Installing faster-whisper package..." -ForegroundColor Yellow
    Write-Host "[!] Note: This may take a couple of minutes due to heavy PyTorch dependencies..." -ForegroundColor Magenta

    # Remove faster_whisper dir if exists to prevent pip target conflicts
    if (Test-Path $FasterWhisperDir) {
        Remove-Item $FasterWhisperDir -Recurse -Force -ErrorAction SilentlyContinue
    }

    # Run pip install --target
    $PipArgs = "-m pip install --target=$SitePackagesDir --upgrade faster-whisper"
    $Process = Start-Process -FilePath ".\cache\engine\python.exe" -ArgumentList $PipArgs -Wait -NoNewWindow -PassThru

    if ($Process.ExitCode -eq 0) {
        Write-Host "[+] faster-whisper installed successfully" -ForegroundColor Green
    } else {
        Write-Error "Failed to install faster-whisper. Exit code: $($Process.ExitCode)"
    }
} else {
    Write-Host "[+] faster-whisper already present and importable in site-packages" -ForegroundColor Gray
}

# 5.1. Sanity Check / Install CUDA 12 support libraries for portable GPU execution on Windows
$CudaLib = Join-Path $SitePackagesDir "nvidia\cublas\bin\cublas64_12.dll"
$CudaValid = $false
if (Test-Path $CudaLib) {
    # Check if we can load the nvidia libraries in python via ctypes
    $TestCuda = Start-Process -FilePath "cache\engine\python.exe" -ArgumentList "-c ""import ctypes; ctypes.CDLL(r'$CudaLib'); print('OK')""" -Wait -NoNewWindow -PassThru -ErrorAction SilentlyContinue
    if ($TestCuda -and $TestCuda.ExitCode -eq 0) {
        $CudaValid = $true
    }
}

if (-not $CudaValid) {
    Write-Host "[PROGRESS] 67 | Installing CUDA 12 support libraries for GPU..." -ForegroundColor Yellow
    $CudaArgs = "-m pip install --target=$SitePackagesDir --upgrade nvidia-cublas-cu12 nvidia-cudnn-cu12"
    $Process = Start-Process -FilePath ".\cache\engine\python.exe" -ArgumentList $CudaArgs -Wait -NoNewWindow -PassThru
    if ($Process.ExitCode -eq 0) {
        Write-Host "[+] CUDA 12 support libraries installed successfully" -ForegroundColor Green
    } else {
        Write-Host "[-] Warning: Failed to install CUDA support libraries. GPU acceleration might require global CUDA Toolkit." -ForegroundColor Yellow
    }
} else {
    Write-Host "[+] CUDA 12 support libraries already present and loadable" -ForegroundColor Gray
}

# 6. Sanity Check / Download FFmpeg static binary
$FfmpegExe = "cache\ffmpeg.exe"
$FfmpegValid = $false
if (Test-Path $FfmpegExe) {
    $TestFfmpeg = Start-Process -FilePath $FfmpegExe -ArgumentList "-version" -Wait -NoNewWindow -PassThru -ErrorAction SilentlyContinue
    if ($TestFfmpeg -and $TestFfmpeg.ExitCode -eq 0) {
        $FfmpegValid = $true
    }
}

if (-not $FfmpegValid) {
    # Make sure cache dir exists
    if (-not (Test-Path "cache")) {
        New-Item -ItemType Directory -Path "cache" -Force | Out-Null
    }

    $FfmpegUrl1 = "https://github.com/GyanD/codexffmpeg/releases/download/7.1/ffmpeg-7.1-essentials_build.zip"
    $FfmpegUrl2 = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
    $FfmpegZipFile = "cache\ffmpeg.zip"

    Write-Host "[PROGRESS] 90 | Downloading static FFmpeg build (High-Speed Mirror)..." -ForegroundColor Yellow
    try {
        Invoke-WebRequest -Uri $FfmpegUrl1 -OutFile $FfmpegZipFile -TimeoutSec 60
    } catch {
        Write-Host "[-] Mirror failed or timed out. Falling back to primary gyan.dev source..." -ForegroundColor Yellow
        Write-Host "[PROGRESS] 90 | Downloading static FFmpeg build (Primary Source)..." -ForegroundColor Yellow
        Invoke-WebRequest -Uri $FfmpegUrl2 -OutFile $FfmpegZipFile
    }
    Write-Host "[PROGRESS] 95 | Extracting FFmpeg..." -ForegroundColor Yellow

    $TempDir = "cache\ffmpeg_temp"
    if (Test-Path $TempDir) { Remove-Item $TempDir -Recurse -Force }
    New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

    Expand-Archive -Path $FfmpegZipFile -DestinationPath $TempDir -Force

    # Locate ffmpeg.exe recursively
    $ExtractedFfmpeg = Get-ChildItem -Path $TempDir -Filter "ffmpeg.exe" -Recurse | Select-Object -First 1
    if ($ExtractedFfmpeg) {
        if (Test-Path $FfmpegExe) {
            Remove-Item $FfmpegExe -Force -ErrorAction SilentlyContinue
        }
        Copy-Item -Path $ExtractedFfmpeg.FullName -Destination $FfmpegExe -Force
        Write-Host "[+] Extracted and copied ffmpeg.exe to cache/" -ForegroundColor Green
    } else {
        Write-Error "Could not find ffmpeg.exe inside extracted archive!"
    }

    # Clean up temp
    Remove-Item $TempDir -Recurse -Force
    Remove-Item $FfmpegZipFile -Force
    Write-Host "[+] Cleaned up FFmpeg temporary files" -ForegroundColor Green
} else {
    Write-Host "[+] FFmpeg binary already present and valid" -ForegroundColor Gray
}

Write-Host "[PROGRESS] 100 | Environment setup completed successfully!"
Write-Host "=== Setup Completed Successfully! ===" -ForegroundColor Green
