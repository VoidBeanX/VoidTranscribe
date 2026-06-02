# PowerShell script to remove the legacy SubtitleThing right-click context menu from the Windows Registry

# Check for administrative privileges (required to modify HKEY_CLASSES_ROOT)
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "Warning: This script requires administrator privileges to modify the registry." -ForegroundColor Yellow
    Write-Host "Requesting elevation..." -ForegroundColor Cyan
    Start-Process powershell -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`"" -Verb RunAs
    Exit
}

# Registry paths for the old context menu
$pathsToDelete = @(
    "Registry::HKEY_CLASSES_ROOT\*\shell\SubtitleThing\command",
    "Registry::HKEY_CLASSES_ROOT\*\shell\SubtitleThing"
)

Write-Host "Cleaning up old registry integration..." -ForegroundColor Cyan

$deletedAny = $false
foreach ($path in $pathsToDelete) {
    if (Test-Path $path) {
        Write-Host "Removing key: $path" -ForegroundColor Green
        Remove-Item -Path $path -Force -Recurse -ErrorAction SilentlyContinue
        $deletedAny = $true
    } else {
        Write-Host "Already clean: $path" -ForegroundColor Gray
    }
}

if ($deletedAny) {
    Write-Host "Legacy Windows Explorer Context Menu integration successfully removed!" -ForegroundColor Green
} else {
    Write-Host "No legacy integration keys found." -ForegroundColor Yellow
}

Write-Host "`nPress any key to close..." -ForegroundColor Gray
$null = [Console]::ReadKey($true)
