package main

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	startupPath := ""
	if len(os.Args) > 1 {
		targetPath := os.Args[1]
		if targetPath == "--help" || targetPath == "-h" {
			fmt.Println("VoidTranscribe - Transcirber")
			fmt.Println("Usage:")
			fmt.Println("  VoidTranscribe.exe              Launch the graphical user interface")
			fmt.Println("  VoidTranscribe.exe <videoPath>  Launch GUI and automatically transcribe video file")
			os.Exit(0)
		}
		startupPath = targetPath
	}

	// Create an instance of the app structure
	app := NewApp()
	if startupPath != "" {
		app.startupVideoPath = startupPath
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "VoidTranscribe",
		Width:  1024,
		Height: 900,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 1}, // Matches sleek dark slate background
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// runHeadlessTranscription executes the isolated Python transcription script silently
func runHeadlessTranscription(videoPath string) {
	// Clean the path
	videoPath = filepath.Clean(videoPath)

	pythonPath, scriptPath, err := findEnginePaths()
	if err != nil {
		// Since it's headless, we can write the error to a log next to the video
		writeErrorLog(videoPath, fmt.Errorf("failed to locate transcription engine: %w", err))
		return
	}

	// Prepare the command
	cmd := exec.Command(pythonPath, scriptPath, videoPath)

	// Windows-Specific Constraint: Hide command prompt window
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	// Prepend local CUDA DLL paths to the process environment PATH variable
	configureCmdEnv(cmd)

	// Run command and capture output/errors
	output, err := cmd.CombinedOutput()
	if err != nil {
		writeErrorLog(videoPath, fmt.Errorf("transcription failed: %w\nOutput: %s", err, string(output)))
		return
	}
}

// findEnginePaths discovers python.exe and transcribe.py inside the portable runtime
func findEnginePaths() (string, string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	exeDir := filepath.Dir(exePath)

	// Primary location: adjacent to the executable (production setup)
	pythonPath := filepath.Join(exeDir, "engine", "python.exe")
	scriptPath := filepath.Join(exeDir, "engine", "transcribe.py")

	if _, err := os.Stat(pythonPath); err == nil {
		return pythonPath, scriptPath, nil
	}

	// Development Fallback 1: check build/bin/engine relative to current working directory
	devPythonPath := filepath.Join(".", "build", "bin", "engine", "python.exe")
	devScriptPath := filepath.Join(".", "build", "bin", "engine", "transcribe.py")
	if _, err := os.Stat(devPythonPath); err == nil {
		absPython, _ := filepath.Abs(devPythonPath)
		absScript, _ := filepath.Abs(devScriptPath)
		return absPython, absScript, nil
	}

	// Development Fallback 2: check engine/ relative to current working directory
	cwdPythonPath := filepath.Join(".", "engine", "python.exe")
	cwdScriptPath := filepath.Join(".", "engine", "transcribe.py")
	if _, err := os.Stat(cwdPythonPath); err == nil {
		absPython, _ := filepath.Abs(cwdPythonPath)
		absScript, _ := filepath.Abs(cwdScriptPath)
		return absPython, absScript, nil
	}

	return pythonPath, scriptPath, fmt.Errorf("engine not found (checked: %s and fallback)", pythonPath)
}

// writeErrorLog writes a debug error file if headless execution fails
func writeErrorLog(videoPath string, err error) {
	logPath := videoPath + ".error.log"
	_ = os.WriteFile(logPath, []byte(err.Error()), 0644)
}

// configureCmdEnv prepends local CUDA support library paths to the execution command's PATH environment variable.
// This is critical for Windows DLL loading under elevation (e.g. running as Administrator).
func configureCmdEnv(cmd *exec.Cmd) {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)
	targetDir := filepath.Join(exeDir, "engine", "Lib", "site-packages")
	if strings.Contains(exePath, "Temp") || strings.Contains(exePath, "go-build") {
		targetDir = filepath.Join(".", "build", "bin", "engine", "Lib", "site-packages")
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return
	}

	env := os.Environ()
	var pathDirs []string
	pathDirs = append(pathDirs, filepath.Join(absTarget, "nvidia", "cublas", "bin"))
	pathDirs = append(pathDirs, filepath.Join(absTarget, "nvidia", "cudnn", "bin"))
	pathDirs = append(pathDirs, filepath.Join(absTarget, "nvidia", "cuda_nvrtc", "bin"))
	newPath := strings.Join(pathDirs, string(os.PathListSeparator))

	pathUpdated := false
	for i, val := range env {
		if strings.HasPrefix(strings.ToUpper(val), "PATH=") {
			parts := strings.SplitN(val, "=", 2)
			env[i] = parts[0] + "=" + newPath + string(os.PathListSeparator) + parts[1]
			pathUpdated = true
			break
		}
	}
	if !pathUpdated {
		env = append(env, "PATH="+newPath)
	}
	cmd.Env = env
}
