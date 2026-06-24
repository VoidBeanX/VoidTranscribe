package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"VoidTranscribe/assets"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct represents the Wails application backend bindings
type App struct {
	ctx              context.Context
	activeCmd        *exec.Cmd
	startupVideoPath string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	wailsruntime.OnFileDrop(ctx, func(x, y int, paths []string) {
		wailsruntime.EventsEmit(ctx, "file-drop", paths)
	})
}

// RequirementsStatus holds status of required dependencies
type RequirementsStatus struct {
	PythonExists        bool   `json:"pythonExists"`
	TranscribeScriptOK  bool   `json:"transcribeScriptOk"`
	FfmpegExists        bool   `json:"ffmpegExists"`
	FasterWhisperReady  bool   `json:"fasterWhisperReady"`
	CudaLibsExists      bool   `json:"cudaLibsExists"`
	IsRegistered        bool   `json:"isRegistered"`
	ModelDirSize        string `json:"modelDirSize"`
}

// CheckRequirements checks the state of the portable environment dependencies
func (a *App) CheckRequirements() RequirementsStatus {
	status := RequirementsStatus{}

	pythonPath, scriptPath, err := findEnginePaths()
	if err == nil {
		status.PythonExists = true
		if _, err := os.Stat(scriptPath); err == nil {
			status.TranscribeScriptOK = true
		}

		// Check if faster-whisper is importable in the python environment
		cmd := exec.Command(pythonPath, "-c", "import faster_whisper")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err == nil {
			status.FasterWhisperReady = true
		}
	}

	// Check for transcribe.py in cache/engine, and if not, create it from assets
	if _, err := os.Stat(scriptPath); err != nil {
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err == nil {
			if err := os.WriteFile(scriptPath, assets.Transcribe, 0644); err == nil {
				status.TranscribeScriptOK = true
			}
		}
	}

	// Check for ffmpeg.exe adjacent to cache or in build/bin/cache
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		ffmpegPath := filepath.Join(exeDir, "cache", "ffmpeg.exe")
		if _, err := os.Stat(ffmpegPath); err == nil {
			status.FfmpegExists = true
		} else {
			// dev fallback
			devFfmpeg := filepath.Join(".", "build", "bin", "cache", "ffmpeg.exe")
			if _, err := os.Stat(devFfmpeg); err == nil {
				status.FfmpegExists = true
			}
		}
	}

	// Check for local CUDA DLL in cache/engine site-packages
	if err == nil {
		exePath, err := os.Executable()
		if err == nil {
			exeDir := filepath.Dir(exePath)
			cudaLibPath := filepath.Join(exeDir, "cache", "engine", "Lib", "site-packages", "nvidia", "cublas", "bin", "cublas64_12.dll")
			if _, err := os.Stat(cudaLibPath); err == nil {
				status.CudaLibsExists = true
			} else {
				// dev fallback
				devCudaLib := filepath.Join(".", "build", "bin", "cache", "engine", "Lib", "site-packages", "nvidia", "cublas", "bin", "cublas64_12.dll")
				if _, err := os.Stat(devCudaLib); err == nil {
					status.CudaLibsExists = true
				}
			}
		}
	}

	// Check if SendTo shortcut is registered
	status.IsRegistered = a.IsSendToRegistered()

	return status
}

// GetSendToPath returns the path to the user's SendTo folder
func getSendToPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("APPDATA environment variable is empty")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "SendTo"), nil
}

// GetSendToLnkPath returns the path to the VoidTranscribe.lnk file in the SendTo folder
func getSendToLnkPath() (string, error) {
	dir, err := getSendToPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "VoidTranscribe.lnk"), nil
}

// IsSendToRegistered checks if the Send To shortcut exists
func (a *App) IsSendToRegistered() bool {
	lnkPath, err := getSendToLnkPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(lnkPath)
	return err == nil
}

// RegisterSendTo creates a shortcut inside the Windows SendTo directory
func (a *App) RegisterSendTo() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	// If in development mode (running from temp or go run), point to the build output path
	if strings.Contains(exePath, "Temp") || strings.Contains(exePath, "go-build") {
		cwd, _ := os.Getwd()
		exePath = filepath.Join(cwd, "build", "bin", "VoidTranscribe.exe")
	}

	lnkPath, err := getSendToLnkPath()
	if err != nil {
		return err
	}

	// Create SendTo shortcut via a PowerShell snippet (COM shell object) to avoid CGO/win32 DLL bindings
	psCmd := fmt.Sprintf(`$WshShell = New-Object -ComObject WScript.Shell; $Shortcut = $WshShell.CreateShortcut('%s'); $Shortcut.TargetPath = '%s'; $Shortcut.IconLocation = '%s'; $Shortcut.Save()`, lnkPath, exePath, exePath)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create SendTo shortcut: %w", err)
	}

	return nil
}

// UnregisterSendTo deletes the shortcut from the SendTo directory
func (a *App) UnregisterSendTo() error {
	lnkPath, err := getSendToLnkPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(lnkPath); err == nil {
		err = os.Remove(lnkPath)
		if err != nil {
			return fmt.Errorf("failed to delete SendTo shortcut: %w", err)
		}
	}
	return nil
}

// TranscribeVideo starts the transcription in a background thread and streams progress to frontend
func (a *App) TranscribeVideo(videoPath string, deviceMode string, formatStyle string, modelSize string, prePromptPath string) (string, error) {
	if a.activeCmd != nil {
		return "", fmt.Errorf("transcription is already running")
	}

	pythonPath, scriptPath, err := findEnginePaths()
	if err != nil {
		return "", fmt.Errorf("transcription engine not found: %w", err)
	}

	videoPath = filepath.Clean(videoPath)
	if _, err := os.Stat(videoPath); err != nil {
		return "", fmt.Errorf("video file not found: %w", err)
	}

	outputPath := videoPath + ".txt"

	// Start Python execution with selected device mode, selected timecode format, selected model and stream results
	cmd := exec.Command(pythonPath, scriptPath, videoPath, "--device", deviceMode, "--format", formatStyle, "--model", modelSize)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	// Prepend local CUDA DLL paths to the process environment PATH variable
	configureCmdEnv(cmd)

	wailsruntime.EventsEmit(a.ctx, "transcription-stdout", fmt.Sprintf("[LOG] [GO] Spawning Python: %s %s %s --device %s --format %s --model %s", pythonPath, scriptPath, videoPath, deviceMode, formatStyle, modelSize))

	a.activeCmd = cmd
	defer func() {
		a.activeCmd = nil
	}()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Goroutine to capture stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			wailsruntime.EventsEmit(a.ctx, "transcription-stdout", line)
		}
	}()

	// Goroutine to capture stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			wailsruntime.EventsEmit(a.ctx, "transcription-stderr", line)
		}
	}()

	// Wait for process to finish
	err = cmd.Wait()
	if err != nil {
		return "", fmt.Errorf("process failed with error: %w", err)
	}

	// Read and return the output file contents
	content, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to read transcription output file: %w", err)
	}

	if prePromptPath != "" {
		prePromptContent, err := os.ReadFile(prePromptPath)
		if err == nil {
			combined := string(prePromptContent) + "\n\n" + string(content)
			_ = os.WriteFile(outputPath, []byte(combined), 0644)
			content = []byte(combined)
		} else {
			wailsruntime.EventsEmit(a.ctx, "transcription-stdout", fmt.Sprintf("[WARN] Failed to read pre-prompt file: %s", err.Error()))
		}
	}

	return string(content), nil
}

// GetStartupVideoPath returns the video path passed as command line argument on startup, if any
func (a *App) GetStartupVideoPath() string {
	path := a.startupVideoPath
	// Clear it so it is only retrieved once
	a.startupVideoPath = ""
	return path
}

// SelectVideoFileDialog opens a native Windows file dialog and returns the selected paths
func (a *App) SelectVideoFileDialog() ([]string, error) {
	return wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Video File(s)",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Video Files (*.mp4; *.mkv; *.avi; *.mov; *.m4v; *.webm)",
				Pattern:     "*.mp4;*.mkv;*.avi;*.mov;*.m4v;*.webm",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
}

// CancelTranscription terminates the active Python process and all its spawned children immediately
func (a *App) CancelTranscription() error {
	if a.activeCmd != nil && a.activeCmd.Process != nil {
		pid := a.activeCmd.Process.Pid

		// Use Windows taskkill with /T (tree) and /F (force) to kill the process and all child processes
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = killCmd.Run()

		a.activeCmd = nil
		wailsruntime.EventsEmit(a.ctx, "transcription-stdout", "[GO] Transcription process was cancelled by the user.")
	}
	return nil
}

// OpenFile opens a local file using the Windows default handler (e.g. Notepad for .txt files)
func (a *App) OpenFile(filePath string) error {
	filePath = filepath.Clean(filePath)
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	cmd := exec.Command("cmd", "/c", "start", "", filePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// OpenFolder opens a folder path directly in Windows Explorer
func (a *App) OpenFolder(folderPath string) error {
	folderPath = filepath.Clean(folderPath)
	fi, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder not found: %w", err)
	}
	if !fi.IsDir() {
		folderPath = filepath.Dir(folderPath)
	}

	cmd := exec.Command("explorer.exe", folderPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// SelectTextFileDialog opens a native Windows file dialog to select a single text file
func (a *App) SelectTextFileDialog() (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Pre-Prompt Text File",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Text Files (*.txt)",
				Pattern:     "*.txt",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
}

var version = "1.0.0"

// GetVersion returns the compiled application version
func (a *App) GetVersion() string {
	return version
}

// GetGpuVramGB queries the total VRAM of the primary GPU using nvidia-smi in Gigabytes.
// Returns 0 if no NVIDIA GPU is detected or if query fails.
func (a *App) GetGpuVramGB() (float64, error) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	valStr := strings.TrimSpace(string(out))
	// In case of multiple GPUs, take the first line
	lines := strings.Split(valStr, "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("no GPU memory info returned")
	}
	firstLine := strings.TrimSpace(lines[0])

	var mib float64
	_, err = fmt.Sscanf(firstLine, "%f", &mib)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU memory: %w", err)
	}

	// Convert MiB to GB (1024 MiB = 1 GiB)
	return mib / 1024.0, nil
}

// shutdown is called by Wails when the application is closing, ensuring background processes are cleaned up
func (a *App) shutdown(ctx context.Context) {
	_ = a.CancelTranscription()
}

// InstallCudaLibraries downloads and installs local CUDA support packages (nvidia-cublas-cu12, nvidia-cudnn-cu12) using pip
func (a *App) InstallCudaLibraries() error {
	if a.activeCmd != nil {
		return fmt.Errorf("another transcription or download process is already running")
	}

	pythonPath, _, err := findEnginePaths()
	if err != nil {
		return fmt.Errorf("python engine not found: %w", err)
	}

	// Determine output directory based on absolute exe location
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exePath)
	targetDir := filepath.Join(exeDir, "cache", "engine", "Lib", "site-packages")
	if strings.Contains(exePath, "Temp") || strings.Contains(exePath, "go-build") {
		// Dev fallback
		targetDir = filepath.Join(".", "build", "bin", "cache", "engine", "Lib", "site-packages")
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}

	writable := isDirWritable(absTarget)

	wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "Starting local CUDA support libraries installation...")
	if !writable {
		wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "[WARN] Target directory is not writable with current user permissions. Requesting Administrator elevation...")
	}
	wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "Executing: pip install --target=cache/engine/Lib/site-packages nvidia-cublas-cu12 nvidia-cudnn-cu12")
	wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "Downloading approx. 1.2GB of files, please wait. This can take a few minutes...")

	var cmd *exec.Cmd
	var tempLog string
	stopTail := make(chan struct{})

	if writable {
		cmd = exec.Command(pythonPath, "-m", "pip", "install", "--target="+absTarget, "nvidia-cublas-cu12", "nvidia-cudnn-cu12")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

		a.activeCmd = cmd
		defer func() {
			a.activeCmd = nil
		}()

		stdout, err := cmd.StdoutPipe()
		if err == nil {
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					wailsruntime.EventsEmit(a.ctx, "cuda-install-log", scanner.Text())
				}
			}()
		}

		stderr, err := cmd.StderrPipe()
		if err == nil {
			go func() {
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "[WARN] "+scanner.Text())
				}
			}()
		}

		err = cmd.Run()
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "CUDA installation failed: "+err.Error())
			return fmt.Errorf("installation failed: %w", err)
		}
	} else {
		// Run with elevation via PowerShell Start-Process -Verb RunAs
		tempLog = filepath.Join(os.TempDir(), "voidtranscribe_cuda_setup.log")
		_ = os.Remove(tempLog)

		// Create empty log file
		if f, err := os.Create(tempLog); err == nil {
			f.Close()
		}

		// Run pip install elevated and redirect output to tempLog
		psCmd := fmt.Sprintf(`Start-Process powershell -ArgumentList "-NoProfile -NonInteractive -Command ""& '%s' -m pip install --target='%s' nvidia-cublas-cu12 nvidia-cudnn-cu12 *>&1 | Out-File -FilePath '%s' -Encoding utf8""" -Verb RunAs -Wait`, pythonPath, absTarget, tempLog)
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

		a.activeCmd = cmd
		defer func() {
			a.activeCmd = nil
		}()

		// Start tailing the log file in a goroutine
		go func() {
			file, err := os.Open(tempLog)
			if err != nil {
				return
			}
			defer file.Close()

			reader := bufio.NewReader(file)
			for {
				line, err := reader.ReadString('\n')
				if err == nil {
					wailsruntime.EventsEmit(a.ctx, "cuda-install-log", strings.TrimSpace(line))
				} else {
					select {
					case <-stopTail:
						// Read any remaining content
						for {
							l, e := reader.ReadString('\n')
							if e != nil {
								if l != "" {
									wailsruntime.EventsEmit(a.ctx, "cuda-install-log", strings.TrimSpace(l))
								}
								break
							}
							wailsruntime.EventsEmit(a.ctx, "cuda-install-log", strings.TrimSpace(l))
						}
						return
					case <-time.After(100 * time.Millisecond):
					}
				}
			}
		}()

		err = cmd.Run()
		close(stopTail)
		_ = os.Remove(tempLog)

		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "CUDA installation failed (elevation error): "+err.Error())
			return fmt.Errorf("elevated installation failed: %w", err)
		}
	}

	wailsruntime.EventsEmit(a.ctx, "cuda-install-log", "CUDA libraries installed successfully!")
	wailsruntime.EventsEmit(a.ctx, "cuda-install-complete", true)
	return nil
}

// VideoValidationResult represents the metadata check outcome
type VideoValidationResult struct {
	IsValid      bool   `json:"isValid"`
	HasAudio     bool   `json:"hasAudio"`
	ErrorMessage string `json:"errorMessage"`
}

// findFfmpegPath discovers ffmpeg.exe inside the portable runtime
func findFfmpegPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exePath)
	ffmpegPath := filepath.Join(exeDir, "cache", "ffmpeg.exe")
	if _, err := os.Stat(ffmpegPath); err == nil {
		return ffmpegPath, nil
	}

	// Dev fallback
	devFfmpeg := filepath.Join(".", "build", "bin", "cache", "ffmpeg.exe")
	if _, err := os.Stat(devFfmpeg); err == nil {
		return filepath.Abs(devFfmpeg)
	}

	return "", fmt.Errorf("ffmpeg.exe not found")
}

// ValidateVideoFile runs ffmpeg on the video file to verify it's a valid media container with audio streams
func (a *App) ValidateVideoFile(videoPath string) VideoValidationResult {
	result := VideoValidationResult{IsValid: false, HasAudio: false}

	ffmpegPath, err := findFfmpegPath()
	if err != nil {
		// If ffmpeg is missing, we can't perform the check; assume valid
		result.IsValid = true
		result.HasAudio = true
		return result
	}

	videoPath = filepath.Clean(videoPath)
	if _, err := os.Stat(videoPath); err != nil {
		result.ErrorMessage = "File does not exist."
		return result
	}

	cmd := exec.Command(ffmpegPath, "-i", videoPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, _ := cmd.CombinedOutput()
	outputStr := string(out)

	// ffmpeg returns exit status 1 when running without output path (as we are doing with -i),
	// so we check output content rather than cmd.Run() error.
	if strings.Contains(outputStr, "Invalid data found when processing input") ||
	   strings.Contains(outputStr, "Duration: N/A") && !strings.Contains(outputStr, "Stream #") {
		result.ErrorMessage = "The file is not a valid video or audio format."
		return result
	}

	if !strings.Contains(outputStr, "Stream #") {
		result.ErrorMessage = "No media streams found in the file."
		return result
	}

	result.IsValid = true

	if strings.Contains(outputStr, "Audio:") {
		result.HasAudio = true
	} else {
		result.ErrorMessage = "The video file does not contain any audio track to transcribe."
	}

	return result
}

// InstallPortableEnvironment runs the embedded setup_env.ps1 script using PowerShell, streaming progress logs to the frontend
func (a *App) InstallPortableEnvironment() error {
	if a.activeCmd != nil {
		return fmt.Errorf("another background process is already running")
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	targetDir := filepath.Dir(exePath)
	if strings.Contains(exePath, "Temp") || strings.Contains(exePath, "go-build") {
		// Dev fallback
		targetDir, _ = filepath.Abs(".")
	}

	writable := isDirWritable(targetDir)

	// Write embedded setup_env.ps1 to a temporary path
	tempScript := filepath.Join(os.TempDir(), "voidtranscribe_setup_env.ps1")
	_ = os.Remove(tempScript)
	if err := os.WriteFile(tempScript, assets.SetupScript, 0755); err != nil {
		return fmt.Errorf("failed to write temporary setup script: %w", err)
	}
	defer func() {
		_ = os.Remove(tempScript)
	}()

	wailsruntime.EventsEmit(a.ctx, "env-install-log", "Starting portable environment installation...")
	if !writable {
		wailsruntime.EventsEmit(a.ctx, "env-install-log", "[WARN] Target directory is not writable with current user permissions. Requesting Administrator elevation...")
	}

	var cmd *exec.Cmd
	var tempLog string
	stopTail := make(chan struct{})

	if writable {
		// Run normally
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", tempScript, "-InstallDir", targetDir)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Dir = targetDir

		a.activeCmd = cmd
		defer func() {
			a.activeCmd = nil
		}()

		stdout, err := cmd.StdoutPipe()
		if err == nil {
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					wailsruntime.EventsEmit(a.ctx, "env-install-log", scanner.Text())
				}
			}()
		}

		stderr, err := cmd.StderrPipe()
		if err == nil {
			go func() {
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					wailsruntime.EventsEmit(a.ctx, "env-install-log", "[WARN] "+scanner.Text())
				}
			}()
		}

		err = cmd.Run()
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "env-install-log", "Installation failed: "+err.Error())
			return fmt.Errorf("installation failed: %w", err)
		}
	} else {
		// Run with elevation via PowerShell Start-Process -Verb RunAs
		tempLog = filepath.Join(os.TempDir(), "voidtranscribe_setup.log")
		_ = os.Remove(tempLog)

		// Create empty log file
		if f, err := os.Create(tempLog); err == nil {
			f.Close()
		}

		// Run setup_env.ps1 elevated and redirect output to tempLog
		psCmd := fmt.Sprintf(`Start-Process powershell -ArgumentList "-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command ""& '%s' -InstallDir '%s' *>&1 | Out-File -FilePath '%s' -Encoding utf8""" -Verb RunAs -Wait`, tempScript, targetDir, tempLog)
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Dir = targetDir

		a.activeCmd = cmd
		defer func() {
			a.activeCmd = nil
		}()

		// Start tailing the log file in a goroutine
		go func() {
			file, err := os.Open(tempLog)
			if err != nil {
				return
			}
			defer file.Close()

			reader := bufio.NewReader(file)
			for {
				line, err := reader.ReadString('\n')
				if err == nil {
					wailsruntime.EventsEmit(a.ctx, "env-install-log", strings.TrimSpace(line))
				} else {
					select {
					case <-stopTail:
						// Read any remaining content
						for {
							l, e := reader.ReadString('\n')
							if e != nil {
								if l != "" {
									wailsruntime.EventsEmit(a.ctx, "env-install-log", strings.TrimSpace(l))
								}
								break
							}
							wailsruntime.EventsEmit(a.ctx, "env-install-log", strings.TrimSpace(l))
						}
						return
					case <-time.After(100 * time.Millisecond):
					}
				}
			}
		}()

		err = cmd.Run()
		close(stopTail)
		_ = os.Remove(tempLog)

		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "env-install-log", "Installation failed (elevation error): "+err.Error())
			return fmt.Errorf("elevated installation failed: %w", err)
		}
	}

	wailsruntime.EventsEmit(a.ctx, "env-install-log", "Environment setup completed successfully!")
	wailsruntime.EventsEmit(a.ctx, "env-install-complete", true)
	return nil
}

// CancelEnvironmentInstallation terminates the active installation process (setup_env.ps1) and all its spawned children immediately
func (a *App) CancelEnvironmentInstallation() error {
	if a.activeCmd != nil && a.activeCmd.Process != nil {
		pid := a.activeCmd.Process.Pid

		// Use Windows taskkill with /T (tree) and /F (force) to kill the process and all child processes
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = killCmd.Run()

		a.activeCmd = nil
		wailsruntime.EventsEmit(a.ctx, "env-install-log", "[ERROR] Installation was cancelled by the user.")
	}
	return nil
}



func isDirWritable(dir string) bool {
	// Walk up to find the first parent directory that exists
	current := dir
	for {
		stat, err := os.Stat(current)
		if err == nil {
			if stat.IsDir() {
				break
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	testFile := filepath.Join(current, ".write_test")
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	_ = os.Remove(testFile)
	return true
}

// AppConfig defines the persistent settings
type AppConfig struct {
	TimecodeFormat     string `json:"timecodeFormat"`
	SelectedModel      string `json:"selectedModel"`
	SelectedDeviceMode string `json:"selectedDeviceMode"`
	PrePromptFilePath  string `json:"prePromptFilePath"`
}

func getConfigPaths() (string, string) {
	// 1. Same path as executable
	exePath, err := os.Executable()
	var primaryPath string
	if err == nil {
		exeDir := filepath.Dir(exePath)
		primaryPath = filepath.Join(exeDir, "config.json")
	}

	// 2. Fallback path in AppData/Local
	var fallbackPath string
	appData, err := os.UserCacheDir()
	if err == nil {
		fallbackPath = filepath.Join(appData, "VoidTranscribe", "config.json")
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			fallbackPath = filepath.Join(home, ".voidtranscribe_config.json")
		}
	}
	return primaryPath, fallbackPath
}

// LoadConfig loads the settings from the configuration file
func (a *App) LoadConfig() AppConfig {
	config := AppConfig{
		TimecodeFormat:     "davinci",
		SelectedModel:      "distil-large-v3",
		SelectedDeviceMode: "cuda",
		PrePromptFilePath:  "",
	}

	primaryPath, fallbackPath := getConfigPaths()

	// Try reading primary first
	if primaryPath != "" {
		if data, err := os.ReadFile(primaryPath); err == nil {
			var loaded AppConfig
			if json.Unmarshal(data, &loaded) == nil {
				return loaded
			}
		}
	}

	// Try reading fallback
	if fallbackPath != "" {
		if data, err := os.ReadFile(fallbackPath); err == nil {
			var loaded AppConfig
			if json.Unmarshal(data, &loaded) == nil {
				return loaded
			}
		}
	}

	return config
}

// SaveConfig saves the settings to the configuration file
func (a *App) SaveConfig(config AppConfig) error {
	primaryPath, fallbackPath := getConfigPaths()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// Attempt to save to primary (adjacent to exe)
	if primaryPath != "" {
		dir := filepath.Dir(primaryPath)
		if isDirWritable(dir) {
			err = os.WriteFile(primaryPath, data, 0644)
			if err == nil {
				return nil
			}
		}
	}

	// Fallback if primary is not writable or failed
	if fallbackPath != "" {
		dir := filepath.Dir(fallbackPath)
		_ = os.MkdirAll(dir, 0755)
		return os.WriteFile(fallbackPath, data, 0644)
	}

	return fmt.Errorf("could not save configuration to any path")
}



