# VoidTranscribe - Portable Environment Setup Script
# Run this from the VoidTranscribe/build/bin directory to prepare the portable runtime

$ErrorActionPreference = "Stop"

# Ensure we are in the correct directory
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

Write-Host "=== VoidTranscribe Dependency Provisioner ===" -ForegroundColor Cyan

# 1. Setup engine directory
if (-not (Test-Path "engine")) {
    New-Item -ItemType Directory -Path "engine" | Out-Null
    Write-Host "[+] Created 'engine' directory" -ForegroundColor Green
}

# 2. Download and Extract Portable Python 3.10.11
$PythonExe = "engine\python.exe"
if (-not (Test-Path $PythonExe)) {
    Write-Host "[*] Downloading Python 3.10.11 embeddable (amd64)..." -ForegroundColor Yellow
    $PythonZipUrl = "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip"
    $PythonZipFile = "python_embed.zip"

    Invoke-WebRequest -Uri $PythonZipUrl -OutFile $PythonZipFile
    Write-Host "[*] Extracting Python to engine/..." -ForegroundColor Yellow
    Expand-Archive -Path $PythonZipFile -DestinationPath "engine" -Force
    Remove-Item $PythonZipFile -Force
    Write-Host "[+] Python embeddable extracted successfully" -ForegroundColor Green
} else {
    Write-Host "[+] Python embeddable already present in engine/" -ForegroundColor Gray
}

# 3. Configure python310._pth (uncomment import site)
$PthFile = "engine\python310._pth"
if (Test-Path $PthFile) {
    Write-Host "[*] Configuring python310._pth..." -ForegroundColor Yellow
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

# 4. Install Pip
$PipInstalled = Test-Path "engine\Scripts\pip.exe"
if (-not $PipInstalled) {
    Write-Host "[*] Installing pip for embeddable python..." -ForegroundColor Yellow
    $GetPipUrl = "https://bootstrap.pypa.io/get-pip.py"
    $GetPipFile = "get-pip.py"

    Invoke-WebRequest -Uri $GetPipUrl -OutFile $GetPipFile
    Write-Host "[*] Executing get-pip.py..." -ForegroundColor Yellow
    # Execute pip setup
    Start-Process -FilePath ".\engine\python.exe" -ArgumentList "get-pip.py" -Wait -NoNewWindow
    Remove-Item $GetPipFile -Force
    Write-Host "[+] Pip installed successfully" -ForegroundColor Green
} else {
    Write-Host "[+] Pip already installed" -ForegroundColor Gray
}

# 5. Install faster-whisper inside site-packages
$SitePackagesDir = "engine\Lib\site-packages"
$FasterWhisperDir = Join-Path $SitePackagesDir "faster_whisper"
if (-not (Test-Path $FasterWhisperDir)) {
    Write-Host "[*] Installing faster-whisper package inside local Lib/site-packages..." -ForegroundColor Yellow
    Write-Host "[!] Note: This may take a couple of minutes due to heavy PyTorch dependencies..." -ForegroundColor Magenta

    # Run pip install --target
    $PipArgs = "-m pip install --target=$SitePackagesDir --upgrade faster-whisper"
    $Process = Start-Process -FilePath ".\engine\python.exe" -ArgumentList $PipArgs -Wait -NoNewWindow -PassThru

    if ($Process.ExitCode -eq 0) {
        Write-Host "[+] faster-whisper installed successfully" -ForegroundColor Green
    } else {
        Write-Error "Failed to install faster-whisper. Exit code: $($Process.ExitCode)"
    }
} else {
    Write-Host "[+] faster-whisper already present in site-packages" -ForegroundColor Gray
}

# 5.1. Install CUDA 12 support libraries for portable GPU execution on Windows
$CudaLib = Join-Path $SitePackagesDir "nvidia\cublas\bin\cublas64_12.dll"
if (-not (Test-Path $CudaLib)) {
    Write-Host "[*] Installing CUDA 12 support libraries (nvidia-cublas-cu12, nvidia-cudnn-cu12) for local GPU acceleration..." -ForegroundColor Yellow
    $CudaArgs = "-m pip install --target=$SitePackagesDir --upgrade nvidia-cublas-cu12 nvidia-cudnn-cu12"
    $Process = Start-Process -FilePath ".\engine\python.exe" -ArgumentList $CudaArgs -Wait -NoNewWindow -PassThru
    if ($Process.ExitCode -eq 0) {
        Write-Host "[+] CUDA 12 support libraries installed successfully" -ForegroundColor Green
    } else {
        Write-Host "[-] Warning: Failed to install CUDA support libraries. GPU acceleration might require global CUDA Toolkit." -ForegroundColor Yellow
    }
} else {
    Write-Host "[+] CUDA 12 support libraries already present" -ForegroundColor Gray
}


# 6. Download FFmpeg static binary
$FfmpegExe = "ffmpeg.exe"
if (-not (Test-Path $FfmpegExe)) {
    Write-Host "[*] Downloading static FFmpeg build (Gyan.dev)..." -ForegroundColor Yellow
    $FfmpegUrl = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
    $FfmpegZipFile = "ffmpeg.zip"

    Invoke-WebRequest -Uri $FfmpegUrl -OutFile $FfmpegZipFile
    Write-Host "[*] Extracting FFmpeg..." -ForegroundColor Yellow

    $TempDir = "ffmpeg_temp"
    if (Test-Path $TempDir) { Remove-Item $TempDir -Recurse -Force }
    New-Item -ItemType Directory -Path $TempDir | Out-Null

    Expand-Archive -Path $FfmpegZipFile -DestinationPath $TempDir -Force

    # Locate ffmpeg.exe recursively
    $ExtractedFfmpeg = Get-ChildItem -Path $TempDir -Filter "ffmpeg.exe" -Recurse | Select-Object -First 1
    if ($ExtractedFfmpeg) {
        Copy-Item -Path $ExtractedFfmpeg.FullName -Destination $FfmpegExe -Force
        Write-Host "[+] Extracted and copied ffmpeg.exe" -ForegroundColor Green
    } else {
        Write-Error "Could not find ffmpeg.exe inside extracted archive!"
    }

    # Clean up temp
    Remove-Item $TempDir -Recurse -Force
    Remove-Item $FfmpegZipFile -Force
    Write-Host "[+] Cleaned up FFmpeg temporary files" -ForegroundColor Green
} else {
    Write-Host "[+] FFmpeg binary already present" -ForegroundColor Gray
}

Write-Host "=== Setup Completed Successfully! ===" -ForegroundColor Green
