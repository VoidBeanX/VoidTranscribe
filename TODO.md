
# AI Implementation Brief: CGO-Free Subtitle Tool with Context Menu Integration

## Objective

Build a standalone Windows desktop application named **VoidTranscribe** using **Go (Wails v2)** for the backend/GUI orchestration, **Tailwind CSS** for the frontend, and an isolated **Portable Python** runtime to handle local GPU-accelerated video transcription via `faster-whisper`. The application must support standard GUI mode and a headless context-menu mode ("Right-Click -> Transcribe").

---

## 1. Project Architecture & Directory Layout

Ensure the final build directory structure exactly matches the template below. The application must run entirely out of this self-contained folder without installing global system dependencies.

```
VoidTranscribe/
├── build/
│   └── bin/
│       ├── VoidTranscribe.exe     (Compiled Wails Go Binary)
│       ├── ffmpeg.exe            (Standalone Windows FFmpeg binary)
│       └── engine/               (Isolated Python Runtime Environment)
│           ├── python.exe        (Windows embeddable python executable)
│           ├── python310._pth    (Modified path configuration file)
│           ├── transcribe.py     (Inference worker script)
│           └── Lib/site-packages/(Target directory for faster-whisper)
├── main.go                       (Application entry point & CLI switch)
├── app.go                        (Wails backend runtime bindings)
├── wails.json                    (Wails configuration file)
└── frontend/                     (Tailwind + Webview UI layer)

```

---

## 2. Step-by-Step Implementation Checklist

### Phase 1: Initialize the Go & Tailwind Monorepo

* [ ] Run `wails init -n VoidTranscribe -t svelte` (or `react`) to scaffold the core project template.
* [ ] Navigate to the `frontend/` folder and initialize Tailwind CSS: `npm install -D tailwindcss postcss autoprefixer && npx tailwindcss init -p`.
* [ ] Configure `frontend/tailwind.config.js` to parse all source components.
* [ ] Inject `@tailwind base; @tailwind components; @tailwind utilities;` into the main application CSS file.

### Phase 2: Create the Go Dual-Mode Orchestrator (`main.go`)

* [ ] Implement an argument parsing check at the very top of `main()`. If `len(os.Args) > 1`, bypass the GUI initialization and immediately execute the headless pipeline.
* [ ] Implement the `runHeadlessTranscription(videoPath string)` function:
* [ ] Programmatically discover the application execution directory via `os.Executable()`.
* [ ] Construct absolute paths to `engine/python.exe` and `engine/transcribe.py`.
* [ ] Define the output text destination as `videoPath + ".txt"`.
* [ ] Use `os/exec` to invoke the embedded Python executable with the target video file path.


* [ ] **Windows-Specific Constraint:** Attach a `SysProcAttr` configuration block containing `HideWindow: true` to the execution command. This guarantees that no visible command prompt flashes on the user's screen during right-click transcription execution.

### Phase 3: Build the Python Inference Worker (`engine/transcribe.py`)

* [ ] Write a lean, dependency-isolated python script inside the `engine/` directory.
* [ ] Import `WhisperModel` from `faster_whisper`.
* [ ] Implement an aggressive execution safety fallback block:
* [ ] Attempt to instantiate `WhisperModel("large-v3", device="cuda", compute_type="float16")`.
* [ ] Catch any native execution errors (e.g., missing CUDA drivers or low VRAM) and automatically fallback to `WhisperModel("large-v3", device="cpu", compute_type="int8")`.


* [ ] Stream the transcription segments sequentially and write them line-by-line into the output text file using `utf-8` encoding. Include timecode brackets formatted as `[start_seconds -> end_seconds]`.

### Phase 4: Construct the Embedded Python Environment

* [ ] Download the official **Windows embeddable package (64-bit)** `.zip` archive for Python 3.10+ from python.org.
* [ ] Extract the archive directly into the `build/bin/engine/` target path.
* [ ] Open the internal `python310._pth` alignment file and uncomment the `import site` line. This step is mandatory to allow the embeddable binary to recognize third-party pip libraries.
* [ ] Execute a targeted pip installation to build the isolated dependency tree:
`python -m pip install --target=./build/bin/engine/Lib/site-packages faster-whisper`

### Phase 5: Windows Context Menu Integration (`.reg`)

* [ ] Create an explicit Windows Registry deployment file named `install_context.reg`.
* [ ] Map the entry to the global file pointer key: `[HKEY_CLASSES_ROOT\*\shell\VoidTranscribe]`.
* [ ] Set the display string label to `"Transcribe with VoidTranscribe"`.
* [ ] Bind the sub-command execution string to point directly to the expected absolute installation path, appending the standard command line parameter: `"C:\Program Files\VoidTranscribe\VoidTranscribe.exe" "%1"`.

### Phase 8: Robust Subprocess Management & Model Configuration

* [ ] Add dynamic download and load logging verbosity to `transcribe.py` (e.g. log Whisper model downloading state and initialization updates).
* [ ] Implement Device Mode settings (GPU Only, Auto, CPU Only) with GPU Only as the default.
* [ ] Pass the device configuration flags from Go into `transcribe.py` (e.g. `--device cuda`, `--device cpu`, `--device auto`).
* [ ] Add a "Cancel Transcription" button in the Svelte GUI that stops/kills the active Python process.
* [ ] Ensure the Python subprocess is terminated if the GUI application exits (e.g. implementing `OnShutdown` hooks in Wails Go backend to kill the process group).

---

## 3. Production Compilation & Packaging Rules

* [ ] When building the application, execute `wails build` from the root directory to guarantee the web assets are correctly minified and embedded directly into the Go binary.
* [ ] Package the resulting `build/bin/` folder assets into an installer configuration (such as Inno Setup or NSIS).
* [ ] Ensure the installer automatically executes the `.reg` entry script during deployment, dynamically updating the file paths to match the user's selected installation directory.
