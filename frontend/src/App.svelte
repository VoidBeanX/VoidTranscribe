<script>
  import { onMount, onDestroy } from 'svelte';
  import {
    CheckRequirements,
    TranscribeVideo,
    SelectVideoFileDialog,
    CancelTranscription,
    InstallCudaLibraries,
    OpenFile,
    GetGpuVramGB,
    GetStartupVideoPath,
    RegisterSendTo,
    UnregisterSendTo,
    ValidateVideoFile
  } from '../wailsjs/go/main/App.js';

  import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime.js';

  // Tabs
  let activeTab = 'transcribe'; // 'transcribe' | 'settings' | 'about'

  // Application State
  let requirements = {
    pythonExists: false,
    transcribeScriptOk: false,
    ffmpegExists: false,
    fasterWhisperReady: false,
    cudaLibsExists: false,
    isRegistered: false
  };
  let checkingRequirements = true;
  let requirementsError = "";

  // CUDA download setup state
  let cudaInstalling = false;
  let cudaInstallComplete = false;
  let cudaInstallSuccess = false;
  let cudaInstallLogs = [];
  let showCudaPrompt = false;
  let cudaConsoleElement;

  // Transcription State
  let selectedVideoPath = "";
  let transcribing = false;
  let progress = 0;
  let statusMessage = "Select a video to get started";
  let transcriptionText = "";
  let selectedDeviceMode = "cuda"; // 'cuda' = GPU Only, 'auto' = Auto, 'cpu' = CPU Only
  let selectedTimecodeFormat = "davinci";

  let selectedModel = "distil-large-v3";
  let showVramWarning = false;
  let detectedVramGB = 0;
  let requiredVramGB = 0;

  let userScrolledUp = false;
  let logsScrolledUp = false;

  function handleScroll(e) {
    const el = e.target;
    const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    userScrolledUp = !isAtBottom;
  }

  function handleLogsScroll(e) {
    const el = e.target;
    const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    logsScrolledUp = !isAtBottom;
  }

  // Real-time logging & segments
  let realtimeLogs = [];
  let realtimeSegments = [];
  let logConsoleElement;
  let segmentViewElement;

  // Timer & Stepper Stage State
  let startTime = null;
  let elapsedSeconds = 0;
  let timerInterval = null;
  let currentStage = 0; // 0 = idle, 1 = loading model, 2 = preprocessing, 3 = transcribing, 4 = complete

  function formatElapsedTime(totalSeconds) {
    const hrs = Math.floor(totalSeconds / 3600);
    const mins = Math.floor((totalSeconds % 3600) / 60);
    const secs = totalSeconds % 60;
    const pad = (num) => String(num).padStart(2, '0');
    if (hrs > 0) {
      return `${pad(hrs)}:${pad(mins)}:${pad(secs)}`;
    }
    return `${pad(mins)}:${pad(secs)}`;
  }

  // Registry operation message
  let registryMessage = "";
  let registryError = "";

  onMount(async () => {
    await runCheckRequirements();
    selectedTimecodeFormat = localStorage.getItem('timecodeFormat') || "davinci";
    selectedModel = localStorage.getItem('selectedModel') || "distil-large-v3";
    selectedDeviceMode = localStorage.getItem('selectedDeviceMode') || "cuda";

    // Register Wails Event Listeners for stdout/stderr streaming
    EventsOn("transcription-stdout", handleStdout);
    EventsOn("transcription-stderr", handleStderr);
    EventsOn("cuda-install-log", handleCudaInstallLog);
    EventsOn("cuda-install-complete", handleCudaInstallComplete);

    // Check for startup video path passed via command line (Explorer context menu)
    try {
      const startupPath = await GetStartupVideoPath();
      if (startupPath) {
        selectedVideoPath = startupPath;
        statusMessage = "Ready to transcribe.";
        // Trigger auto-start of transcription after a brief UI mount timeout
        setTimeout(() => {
          startTranscription();
        }, 300);
      }
    } catch (err) {
      console.error("Failed to retrieve startup video path:", err);
    }
  });

  onDestroy(() => {
    EventsOff("transcription-stdout");
    EventsOff("transcription-stderr");
    EventsOff("cuda-install-log");
    EventsOff("cuda-install-complete");
  });

  // Auto-scroll logs and segments when updated
  $: if (transcribing && !logsScrolledUp && realtimeLogs && logConsoleElement) {
    setTimeout(() => {
      if (logConsoleElement) {
        logConsoleElement.scrollTop = logConsoleElement.scrollHeight;
      }
    }, 50);
  }

  $: if (selectedTimecodeFormat) {
    localStorage.setItem('timecodeFormat', selectedTimecodeFormat);
  }

  $: if (selectedModel) {
    localStorage.setItem('selectedModel', selectedModel);
  }

  $: if (selectedDeviceMode) {
    localStorage.setItem('selectedDeviceMode', selectedDeviceMode);
  }

  $: if (transcribing && !userScrolledUp && realtimeSegments && segmentViewElement) {
    setTimeout(() => {
      if (segmentViewElement) {
        segmentViewElement.scrollTop = segmentViewElement.scrollHeight;
      }
    }, 50);
  }

  $: if (cudaInstallLogs && cudaConsoleElement) {
    setTimeout(() => {
      cudaConsoleElement.scrollTop = cudaConsoleElement.scrollHeight;
    }, 50);
  }

  async function runCheckRequirements() {
    checkingRequirements = true;
    requirementsError = "";
    try {
      requirements = await CheckRequirements();
      if (!requirements.pythonExists || !requirements.ffmpegExists || !requirements.fasterWhisperReady) {
        requirementsError = "Incomplete portable environment setup. Please run setup_env.ps1 to install Python, FFmpeg, and dependencies.";
      }
    } catch (err) {
      console.error(err);
      requirementsError = "Failed to communicate with the Go backend: " + err.message;
    } finally {
      checkingRequirements = false;
    }
  }

  function handleStdout(line) {
    // Check for special prefix tags from transcribe.py
    if (line.startsWith("[PROGRESS] ")) {
      const pct = parseFloat(line.substring(11).trim());
      if (!isNaN(pct)) {
        progress = pct;
      }
    } else if (line.startsWith("[SEGMENT] ")) {
      const segmentStr = line.substring(10).trim();
      // Format: [start -> end] text
      const timecodeEndIdx = segmentStr.indexOf("]");
      if (timecodeEndIdx > 0) {
        const timecode = segmentStr.substring(0, timecodeEndIdx + 1);
        const text = segmentStr.substring(timecodeEndIdx + 1).trim();
        realtimeSegments = [...realtimeSegments, { timecode, text }];
      } else {
        realtimeSegments = [...realtimeSegments, { timecode: "", text: segmentStr }];
      }
    } else if (line.startsWith("[LOG] ")) {
      const msg = line.substring(6).trim();
      realtimeLogs = [...realtimeLogs, `[INFO] ${msg}`];

      // Update transcription stepper stage dynamically
      if (msg.includes("Stage 1/3:")) {
        currentStage = 1;
      } else if (msg.includes("Stage 2/3:")) {
        currentStage = 2;
      } else if (msg.includes("Stage 3/3:")) {
        currentStage = 3;
      }
    } else {
      realtimeLogs = [...realtimeLogs, line];
    }
  }

  function handleStderr(line) {
    // Python traceback or standard library warnings
    if (line.trim()) {
      realtimeLogs = [...realtimeLogs, `[WARN] ${line}`];
    }
  }

  function handleCudaInstallLog(line) {
    cudaInstallLogs = [...cudaInstallLogs, line];
  }

  async function handleCudaInstallComplete() {
    cudaInstalling = false;
    cudaInstallComplete = true;
    cudaInstallSuccess = true;
    await runCheckRequirements(); // Refresh requirements state
  }

  async function startCudaInstallation() {
    cudaInstalling = true;
    cudaInstallComplete = false;
    cudaInstallSuccess = false;
    cudaInstallLogs = ["[GUI] Initiating local CUDA support library download and setup..."];
    try {
      await InstallCudaLibraries();
      cudaInstallComplete = true;
      cudaInstallSuccess = true;
      await runCheckRequirements();
    } catch (err) {
      console.error(err);
      cudaInstallLogs = [...cudaInstallLogs, `[ERROR] CUDA installation failed: ${err.message || err}`];
      cudaInstallComplete = true;
      cudaInstallSuccess = false;
    } finally {
      cudaInstalling = false;
    }
  }

  async function browseVideo() {
    if (transcribing) return;

    try {
      const result = await SelectVideoFileDialog();
      if (result) {
        selectedVideoPath = result;
        statusMessage = "Ready to transcribe.";
        // Reset old state
        progress = 0;
        transcriptionText = "";
        realtimeLogs = [];
        realtimeSegments = [];
      }
    } catch (err) {
      statusMessage = "Error selecting file: " + err;
    }
  }

  async function startTranscription(forceBypass = false) {
    if (!selectedVideoPath || transcribing) return;

    // Verify file is a valid media format with audio using ffmpeg metadata validation
    try {
      statusMessage = "Verifying file format...";
      realtimeLogs = ["[GO] Verifying video file format and audio track presence..."];

      const validation = await ValidateVideoFile(selectedVideoPath);
      if (!validation.isValid) {
        statusMessage = "Error: " + validation.errorMessage;
        realtimeLogs = [...realtimeLogs, `[ERROR] Validation failed: ${validation.errorMessage}`];
        alert("Invalid file: " + validation.errorMessage);
        return;
      }
      if (!validation.hasAudio) {
        statusMessage = "Error: " + validation.errorMessage;
        realtimeLogs = [...realtimeLogs, `[ERROR] Validation failed: ${validation.errorMessage}`];
        alert("Invalid file: " + validation.errorMessage);
        return;
      }

      realtimeLogs = [...realtimeLogs, "[INFO] File format validation successful. Audio stream detected."];
    } catch (err) {
      console.error("File format validation failed:", err);
      // Fallback: if validation itself fails (e.g. backend error), proceed
    }

    // Strict Device Verification Check: Prompt popup if local CUDA DLLs are missing and GPU mode is selected
    if (selectedDeviceMode === 'cuda' && !requirements.cudaLibsExists) {
      showCudaPrompt = true;
      return;
    }

    // GPU VRAM Requirement precheck
    if (!forceBypass && selectedDeviceMode !== 'cpu') {
      const vramReqs = {
        'distil-small.en': 1.5,
        'distil-medium.en': 2.0,
        'distil-large-v2': 3.0,
        'distil-large-v3': 3.0
      };
      const req = vramReqs[selectedModel] || 3.0;
      requiredVramGB = req;

      try {
        const vramGB = await GetGpuVramGB();
        detectedVramGB = vramGB;
        // Apply a small 0.2 GB tolerance to prevent false-positives for cards reporting slightly less than standard marketed sizes (e.g. 7.9 GB on an 8 GB card)
        if (vramGB > 0 && vramGB < (req - 0.2)) {
          showVramWarning = true;
          return;
        }
      } catch (err) {
        console.error("VRAM query failed:", err);
      }
    }

    showVramWarning = false;
    transcribing = true;
    progress = 0;
    currentStage = 1; // Stage 1: Load Model
    transcriptionText = "";
    realtimeLogs = [];
    realtimeSegments = [];
    statusMessage = "Initializing Whisper engine...";

    userScrolledUp = false;
    logsScrolledUp = false;

    // Start elapsed timer
    elapsedSeconds = 0;
    startTime = Date.now();
    if (timerInterval) clearInterval(timerInterval);
    timerInterval = setInterval(() => {
      elapsedSeconds = Math.floor((Date.now() - startTime) / 1000);
    }, 1000);

    try {
      realtimeLogs = [...realtimeLogs, "[GO] Starting Python runner process..."];
      const result = await TranscribeVideo(selectedVideoPath, selectedDeviceMode, selectedTimecodeFormat, selectedModel);
      transcriptionText = result;
      statusMessage = "Transcription completed successfully!";
      progress = 100;
      currentStage = 4; // Stage 4: Complete
    } catch (err) {
      console.error(err);
      currentStage = 0;
      if (err.includes && err.includes("cancelled")) {
        statusMessage = "Transcription cancelled.";
      } else {
        statusMessage = "Transcription failed: " + err;
        realtimeLogs = [...realtimeLogs, `[ERROR] Go Backend: ${err}`];
      }
    } finally {
      transcribing = false;
      if (timerInterval) {
        clearInterval(timerInterval);
        timerInterval = null;
      }
    }
  }

  function bypassVramAndRun() {
    showVramWarning = false;
    startTranscription(true);
  }

  async function cancelTranscriptionProcess() {
    try {
      statusMessage = "Cancelling transcription...";
      realtimeLogs = [...realtimeLogs, "[GO] Requesting process cancellation..."];
      await CancelTranscription();
      transcribing = false;
      progress = 0;
    } catch (err) {
      console.error("Cancel failed:", err);
      realtimeLogs = [...realtimeLogs, `[ERROR] Cancel failed: ${err}`];
    }
  }

  async function openTranscriptFile() {
    if (!selectedVideoPath) return;
    const path = selectedVideoPath + ".txt";
    try {
      await OpenFile(path);
    } catch (err) {
      console.error(err);
      realtimeLogs = [...realtimeLogs, `[ERROR] Failed to open transcript file: ${err.message || err}`];
    }
  }

  async function handleRegisterRegistry() {
    registryMessage = "";
    registryError = "";
    try {
      await RegisterSendTo();
      registryMessage = "SendTo shortcut successfully created!";
      await runCheckRequirements(); // Refresh state
    } catch (err) {
      console.error(err);
      registryError = err.message || "Failed to create SendTo shortcut.";
    }
  }

  async function handleUnregisterRegistry() {
    registryMessage = "";
    registryError = "";
    try {
      await UnregisterSendTo();
      registryMessage = "SendTo shortcut successfully removed.";
      await runCheckRequirements(); // Refresh state
    } catch (err) {
      console.error(err);
      registryError = err.message || "Failed to delete SendTo shortcut.";
    }
  }

  function getFileName(path) {
    if (!path) return "";
    return path.split('\\').pop().split('/').pop();
  }
</script>

<div class="h-screen w-screen bg-slate-950 text-slate-100 flex flex-col font-sans overflow-hidden select-none" style="background: radial-gradient(circle at 80% 20%, rgba(99, 102, 241, 0.15), transparent), radial-gradient(circle at 10% 80%, rgba(168, 85, 247, 0.12), transparent), #090d16;">

  <!-- Sleek Top Navbar -->
  <header class="h-16 px-8 flex items-center justify-between border-b border-slate-900 bg-slate-950/40 backdrop-blur-md z-10 shrink-0">
    <div class="flex items-center space-x-3">
      <div class="h-9 w-9 rounded-lg bg-gradient-to-tr from-indigo-500 to-purple-600 flex items-center justify-center shadow-lg shadow-indigo-500/20">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M7 8h10M7 12h4m1 8l-4-4H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-3l-4 4z" />
        </svg>
      </div>
      <div>
        <h1 class="text-lg font-bold tracking-tight bg-gradient-to-r from-indigo-200 via-indigo-100 to-white bg-clip-text text-transparent">VoidTranscribe</h1>
        <p class="text-xs text-slate-400 -mt-1">Subtitle Transcription Tool</p>
      </div>
    </div>

    <!-- Mode / CUDA Status Badge -->
    <div class="flex items-center space-x-4">
      {#if !checkingRequirements && !requirements.cudaLibsExists}
        <button
          on:click={() => showCudaPrompt = true}
          class="text-xs px-2.5 py-1 rounded-full bg-amber-950/80 hover:bg-amber-900/80 text-amber-400 border border-amber-900/60 flex items-center space-x-1.5 shadow-md hover:scale-102 transition-all active:scale-98 cursor-pointer"
        >
          <span class="h-1.5 w-1.5 rounded-full bg-amber-400 animate-pulse"></span>
          <span>CUDA DLLs Missing</span>
        </button>
      {/if}

      {#if checkingRequirements}
        <span class="text-xs px-2.5 py-1 rounded-full bg-slate-800 text-slate-400 flex items-center space-x-1.5 animate-pulse">
          <span class="h-1.5 w-1.5 rounded-full bg-slate-400"></span>
          <span>Checking engine...</span>
        </span>
      {:else if requirements.fasterWhisperReady}
        <span class="text-xs px-2.5 py-1 rounded-full bg-emerald-950/80 text-emerald-400 border border-emerald-900/60 flex items-center space-x-1.5 shadow-md shadow-emerald-950/10">
          <span class="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
          <span>Whisper Engine Ready</span>
        </span>
      {:else}
        <span class="text-xs px-2.5 py-1 rounded-full bg-red-950/80 text-red-400 border border-red-900/60 flex items-center space-x-1.5 shadow-md shadow-red-950/10">
          <span class="h-1.5 w-1.5 rounded-full bg-red-400"></span>
          <span>Engine Offline</span>
        </span>
      {/if}

      <!-- Tab Buttons -->
      <nav class="flex space-x-1 bg-slate-900/60 p-1 rounded-lg border border-slate-800/40">
        <button
          on:click={() => activeTab = 'transcribe'}
          class="px-4 py-1.5 rounded-md text-xs font-semibold transition-all duration-200 {activeTab === 'transcribe' ? 'bg-indigo-600 text-white shadow-md shadow-indigo-600/10' : 'text-slate-400 hover:text-slate-200'}"
        >
          Transcribe
        </button>
        <button
          on:click={() => activeTab = 'settings'}
          class="px-4 py-1.5 rounded-md text-xs font-semibold transition-all duration-200 {activeTab === 'settings' ? 'bg-indigo-600 text-white shadow-md shadow-indigo-600/10' : 'text-slate-400 hover:text-slate-200'}"
        >
          Settings
        </button>
      </nav>
    </div>
  </header>

  <!-- Main Display Body -->
  <main class="flex-1 p-8 overflow-hidden flex flex-col items-center justify-center">

    <!-- Tab 1: Transcription Studio -->
    {#if activeTab === 'transcribe'}
      <div class="w-full max-w-4xl h-full flex flex-col space-y-6">

        <!-- Requirements Alert banner -->
        {#if !checkingRequirements && requirementsError}
          <div class="px-5 py-3.5 rounded-xl bg-amber-950/30 border border-amber-900/50 text-amber-300 text-xs flex items-start space-x-3 shadow-lg">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-amber-400 shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <div>
              <span class="font-bold block mb-0.5">Portable dependencies missing</span>
              {requirementsError}
            </div>
            <button on:click={runCheckRequirements} class="ml-auto px-2.5 py-1 rounded bg-amber-500/20 hover:bg-amber-500/30 text-amber-200 font-semibold border border-amber-500/30 transition-all">
              Retry Check
            </button>
          </div>
        {/if}

        <!-- Workspace Layout -->
        <div class="flex-1 grid grid-cols-5 gap-6 min-h-0">

          <!-- Left Part: Selector and Controls -->
          <div class="col-span-2 flex flex-col space-y-5">

            <!-- Beautiful Glass Dropzone -->
            <div
              on:click={browseVideo}
              class="flex-1 rounded-2xl border-2 border-dashed border-slate-800 bg-slate-900/25 hover:bg-slate-900/40 hover:border-indigo-500/50 transition-all duration-300 flex flex-col items-center justify-center p-6 cursor-pointer group text-center backdrop-blur-sm relative"
            >
              <div class="h-16 w-16 rounded-2xl bg-slate-900/60 border border-slate-800 flex items-center justify-center text-slate-400 group-hover:text-indigo-400 group-hover:border-indigo-500/30 group-hover:scale-110 shadow-inner transition-all duration-300 mb-4">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-8 w-8" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z" />
                </svg>
              </div>
              <span class="text-sm font-semibold text-slate-300 group-hover:text-slate-100 transition-colors">Select video file</span>
              <span class="text-xs text-slate-500 mt-1 max-w-[200px] leading-relaxed">MP4, MKV, AVI, MOV or WEBM local files</span>

              {#if selectedVideoPath}
                <div class="absolute inset-x-4 bottom-4 py-2 px-3 rounded-lg bg-slate-950/80 border border-slate-800/80 flex items-center justify-between text-left">
                  <div class="min-w-0 flex-1">
                    <span class="text-xs font-semibold text-indigo-300 block truncate">{getFileName(selectedVideoPath)}</span>
                    <span class="text-[10px] text-slate-500 block truncate">{selectedVideoPath}</span>
                  </div>
                  <button on:click|stopPropagation={() => selectedVideoPath = ""} class="h-6 w-6 rounded hover:bg-red-950/40 text-slate-400 hover:text-red-400 flex items-center justify-center transition-all ml-2">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>
              {/if}
            </div>

            <!-- Action panel -->
            <div class="p-5 rounded-2xl bg-slate-900/35 border border-slate-900 backdrop-blur-sm flex flex-col space-y-4">
              <!-- Timecode Format dropdown -->
              <div class="flex items-center justify-between text-xs">
                <span class="font-medium text-slate-400">Timecode Style:</span>
                <select
                  bind:value={selectedTimecodeFormat}
                  disabled={transcribing}
                  class="bg-slate-950 border border-slate-800 text-indigo-300 font-semibold px-2 py-1 rounded focus:outline-none focus:border-indigo-500 disabled:opacity-50 transition-all cursor-pointer text-xs animate-none"
                >
                  <option value="davinci">DaVinci Resolve [HH:MM:SS:FF]</option>
                  <option value="premiere">Adobe Premiere [HH:MM:SS:FF]</option>
                  <option value="avid">Avid Locators [HH:MM:SS:FF]</option>
                  <option value="fcp">Final Cut Pro [Range FF]</option>
                  <option value="seconds">Seconds [start -> end]</option>
                  <option value="srt">SubRip SRT [ms Arrow]</option>
                  <option value="vtt">WebVTT VTT [ms Arrow]</option>
                </select>
              </div>

              <!-- Output Location quick view -->
              <div class="text-[10px] text-slate-500 leading-normal space-y-1 py-1 border-t border-slate-900">
                <div class="flex justify-between">
                  <span>Output Location:</span>
                  <span class="text-slate-400 text-right truncate max-w-[120px]" title="Next to video file">Adjacent (.txt)</span>
                </div>
              </div>

              <!-- Main Button -->
              {#if !transcribing}
                <button
                  on:click={startTranscription}
                  disabled={!selectedVideoPath || requirementsError !== ""}
                  class="w-full py-3 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white font-bold text-sm tracking-wide shadow-lg shadow-indigo-600/20 disabled:opacity-40 disabled:pointer-events-none transition-all duration-300 active:scale-98 flex items-center justify-center space-x-2"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                    <path stroke-linecap="round" stroke-linejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span>Start Transcription</span>
                </button>
              {:else}
                <div class="w-full flex flex-col space-y-2">
                  <div class="flex justify-between text-xs">
                    <span class="font-bold text-indigo-400 animate-pulse">{statusMessage}</span>
                    <span class="font-bold text-slate-300">{progress.toFixed(1)}%</span>
                  </div>

                  <!-- Glowing progress track -->
                  <div class="h-2 w-full rounded-full bg-slate-950 overflow-hidden border border-slate-900/60 p-0.5">
                    <div
                      class="h-full rounded-full bg-gradient-to-r from-indigo-500 via-indigo-400 to-purple-500 transition-all duration-300 shadow-md shadow-indigo-500/20"
                      style="width: {progress}%"
                    ></div>
                  </div>

                  <!-- Cancel Button -->
                  <button
                    on:click={cancelTranscriptionProcess}
                    class="w-full py-1.5 mt-1 rounded-lg bg-red-950/30 hover:bg-red-950/60 text-red-400 font-semibold text-xs tracking-wider border border-red-900/30 hover:border-red-700/40 transition-all active:scale-98 flex items-center justify-center space-x-1.5"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
                    </svg>
                    <span>Cancel Transcription</span>
                  </button>
                </div>
              {/if}
            </div>

          </div>

          <!-- Right Part: Real-Time Terminal Output and Live Text Stream -->
          <div class="col-span-3 flex flex-col space-y-5 min-h-0">

            <!-- Transcription Status Dashboard Card -->
            {#if transcribing || transcriptionText || currentStage > 0}
              <div class="rounded-2xl bg-slate-900/25 border border-slate-900 backdrop-blur-sm p-5 flex flex-col space-y-3 shrink-0 transition-all duration-300">

                <!-- Top Row: Stage Stepper & Timer -->
                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">

                  <!-- Stepper Stages -->
                  <div class="flex items-center space-x-2 text-[10px] sm:text-xs">
                    <!-- Stage 1: Load Model -->
                    <div class="flex items-center space-x-1.5">
                      <div class="h-5 w-5 rounded-full flex items-center justify-center border font-bold text-[10px] transition-all
                        {currentStage > 1 ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-400' :
                         currentStage === 1 ? 'bg-indigo-950/40 border-indigo-500 text-indigo-400 shadow-md shadow-indigo-500/20 animate-pulse' :
                         'bg-slate-950/40 border-slate-800 text-slate-500'}"
                      >
                        {#if currentStage > 1}
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                          </svg>
                        {:else}
                          1
                        {/if}
                      </div>
                      <span class="font-semibold transition-all
                        {currentStage > 1 ? 'text-emerald-400/80' :
                         currentStage === 1 ? 'text-indigo-300 font-bold' :
                         'text-slate-500'}"
                      >Model Load</span>
                    </div>

                    <!-- Arrow separator -->
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-slate-800" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" />
                    </svg>

                    <!-- Stage 2: Audio Preprocess -->
                    <div class="flex items-center space-x-1.5">
                      <div class="h-5 w-5 rounded-full flex items-center justify-center border font-bold text-[10px] transition-all
                        {currentStage > 2 ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-400' :
                         currentStage === 2 ? 'bg-indigo-950/40 border-indigo-500 text-indigo-400 shadow-md shadow-indigo-500/20 animate-pulse' :
                         'bg-slate-950/40 border-slate-800 text-slate-500'}"
                      >
                        {#if currentStage > 2}
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                          </svg>
                        {:else}
                          2
                        {/if}
                      </div>
                      <span class="font-semibold transition-all
                        {currentStage > 2 ? 'text-emerald-400/80' :
                         currentStage === 2 ? 'text-indigo-300 font-bold' :
                         'text-slate-500'}"
                      >Preprocessing</span>
                    </div>

                    <!-- Arrow separator -->
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-slate-800" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" />
                    </svg>

                    <!-- Stage 3: Transcribe -->
                    <div class="flex items-center space-x-1.5">
                      <div class="h-5 w-5 rounded-full flex items-center justify-center border font-bold text-[10px] transition-all
                        {currentStage > 3 ? 'bg-emerald-950/40 border-emerald-500/30 text-emerald-400' :
                         currentStage === 3 ? 'bg-indigo-950/40 border-indigo-500 text-indigo-400 shadow-md shadow-indigo-500/20 animate-pulse' :
                         'bg-slate-950/40 border-slate-800 text-slate-500'}"
                      >
                        {#if currentStage > 3}
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                          </svg>
                        {:else}
                          3
                        {/if}
                      </div>
                      <span class="font-semibold transition-all
                        {currentStage > 3 ? 'text-emerald-400/80' :
                         currentStage === 3 ? 'text-indigo-300 font-bold' :
                         'text-slate-500'}"
                      >Transcribe</span>
                    </div>
                  </div>

                  <!-- Timer Widget -->
                  <div class="flex items-center space-x-2 text-xs ml-auto sm:ml-0 bg-slate-950/50 border border-slate-900 px-2.5 py-1 rounded-lg shrink-0">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 text-indigo-400 {transcribing ? 'animate-spin' : ''}" fill="none" viewBox="0 0 24 24" stroke="currentColor" style="animation-duration: 8s;">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    <span class="text-slate-500 font-medium">Elapsed:</span>
                    <span class="font-mono text-indigo-300 font-bold tracking-wider">{formatElapsedTime(elapsedSeconds)}</span>
                  </div>

                </div>

                <!-- Middle Row: Progress bar & Percent -->
                <div class="flex flex-col space-y-1.5 border-b border-slate-900/60 pb-3 mb-1">
                  <div class="flex justify-between items-center text-xs">
                    <span class="text-slate-400 font-semibold">
                      {#if currentStage === 1}
                        Initializing dependencies & caching Whisper model...
                      {:else if currentStage === 2}
                        Extracting audio track & running Voice Activity Detection (VAD)...
                      {:else if currentStage === 3}
                        Generating subtitles & timecodes...
                      {:else if currentStage === 4}
                        Done! File written adjacent to input.
                      {:else}
                        Starting Python worker runner...
                      {/if}
                    </span>
                    <span class="font-bold text-indigo-300 font-mono">{progress.toFixed(1)}%</span>
                  </div>

                  <div class="h-2 w-full rounded-full bg-slate-950 overflow-hidden border border-slate-900/60 p-0.5 relative">
                    <div
                      class="h-full rounded-full bg-gradient-to-r from-indigo-500 via-indigo-400 to-purple-500 transition-all duration-300 shadow-md shadow-indigo-500/20"
                      style="width: {progress}%"
                    ></div>
                  </div>
                </div>

                <!-- Open File Button (when finished) -->
                {#if currentStage === 4 && transcriptionText}
                  <button
                    on:click={openTranscriptFile}
                    class="w-full py-2.5 rounded-xl bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 text-white font-bold text-xs tracking-wider shadow-lg shadow-emerald-600/20 transition-all active:scale-98 flex items-center justify-center space-x-2 border border-emerald-500/20"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                      <path stroke-linecap="round" stroke-linejoin="round" d="M9 13h6" />
                    </svg>
                    <span>Open Transcript File (.txt)</span>
                  </button>
                {/if}

              </div>
            {/if}

            <!-- Live Transcription Text View -->
            <div class="flex-1 rounded-2xl bg-slate-950/60 border border-slate-900/80 backdrop-blur-sm p-5 flex flex-col min-h-0">
              <div class="flex items-center justify-between border-b border-slate-900 pb-3 mb-3 shrink-0">
                <span class="text-xs font-bold uppercase tracking-wider text-slate-400 flex items-center space-x-1.5">
                  <span class="h-1.5 w-1.5 rounded-full bg-indigo-500"></span>
                  <span>Live Transcript Preview</span>
                </span>
                {#if transcriptionText}
                  <span class="text-[10px] text-emerald-400 font-bold bg-emerald-950/40 border border-emerald-900/40 px-2 py-0.5 rounded">
                    Output Saved Adjacent
                  </span>
                {/if}
              </div>

              <!-- Content wrapper -->
              <div
                bind:this={segmentViewElement}
                on:scroll={handleScroll}
                class="flex-1 overflow-y-auto space-y-3.5 pr-2 custom-scrollbar text-sm"
              >
                {#if realtimeSegments.length === 0}
                  <div class="h-full w-full flex flex-col items-center justify-center text-slate-600 text-xs text-center py-20">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-8 w-8 mb-2 opacity-40" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h7" />
                    </svg>
                    <span>Transcribed text will stream here in real-time</span>
                  </div>
                {:else}
                  {#each realtimeSegments as seg}
                    <div class="flex items-start space-x-3.5 group hover:bg-slate-900/20 p-1.5 rounded-lg transition-all">
                      <span class="text-[11px] font-semibold font-mono text-indigo-400 bg-indigo-950/30 border border-indigo-900/30 px-2 py-0.5 rounded select-all shrink-0 mt-0.5">
                        {seg.timecode}
                      </span>
                      <span class="text-slate-200 select-text leading-relaxed">{seg.text}</span>
                    </div>
                  {/each}
                {/if}
              </div>
            </div>

            <!-- Developer Log / Terminal Output -->
            <div class="h-44 rounded-2xl bg-slate-950 border border-slate-900 p-4 flex flex-col min-h-0 shrink-0">
              <div class="text-[10px] font-bold uppercase tracking-wider text-slate-500 pb-2 border-b border-slate-900 shrink-0 flex justify-between items-center">
                <span>Process logs (Engine Console)</span>
                {#if transcribing}
                  <span class="h-1.5 w-1.5 rounded-full bg-amber-500 animate-ping"></span>
                {/if}
              </div>
              <div
                bind:this={logConsoleElement}
                on:scroll={handleLogsScroll}
                class="flex-1 overflow-y-auto font-mono text-[11px] text-slate-400 mt-2 space-y-1.5 pr-2 custom-scrollbar text-left select-all"
              >
                {#if realtimeLogs.length === 0}
                  <span class="text-slate-700 italic">No process logs. Start transcription to view detailed orchestration logs.</span>
                {:else}
                  {#each realtimeLogs as log}
                    {#if log.includes("[INFO]")}
                      <div class="text-indigo-300">{log}</div>
                    {:else if log.includes("[ERROR]")}
                      <div class="text-red-400 font-bold">{log}</div>
                    {:else if log.includes("[WARN]")}
                      <div class="text-amber-400">{log}</div>
                    {:else if log.includes("[GO]")}
                      <div class="text-teal-400">{log}</div>
                    {:else}
                      <div>{log}</div>
                    {/if}
                  {/each}
                {/if}
              </div>
            </div>

          </div>

        </div>

      </div>
    {/if}

    <!-- Tab 2: Settings -->
    {#if activeTab === 'settings'}
      <div class="w-full max-w-2xl bg-slate-900/25 border border-slate-900/60 rounded-3xl p-8 backdrop-blur-md space-y-6">

        <div class="flex items-center space-x-4">
          <div class="h-12 w-12 rounded-xl bg-indigo-600/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </div>
          <div>
            <h2 class="text-lg font-bold tracking-tight">Application & Integration Settings</h2>
            <p class="text-xs text-slate-400">Configure transcription models, devices, and Explorer shortcuts</p>
          </div>
        </div>

        <div class="space-y-6">
          <!-- Whisper Configuration Panel -->
          <div class="p-5 rounded-2xl bg-slate-950/40 border border-slate-900 flex flex-col space-y-4">
            <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">Model & Hardware Settings</h3>

            <!-- Target Model Select -->
            <div class="flex items-center justify-between text-xs">
              <span class="font-medium text-slate-300">Target Model:</span>
              <select
                bind:value={selectedModel}
                disabled={transcribing}
                class="bg-slate-950 border border-slate-800 text-indigo-300 font-semibold px-2 py-1.5 rounded focus:outline-none focus:border-indigo-500 disabled:opacity-50 transition-all cursor-pointer text-xs"
              >
                <option value="distil-small.en">Distil-Small (Req: 1.5GB VRAM)</option>
                <option value="distil-medium.en">Distil-Medium (Req: 2.0GB VRAM)</option>
                <option value="distil-large-v2">Distil-Large-V2 (Req: 3.0GB VRAM)</option>
                <option value="distil-large-v3">Distil-Large-V3 (Req: 3.0GB VRAM)</option>
              </select>
            </div>

            <!-- Inference Device Select -->
            <div class="flex items-center justify-between text-xs border-t border-slate-900/60 pt-3">
              <span class="font-medium text-slate-300">Inference Device:</span>
              <select
                bind:value={selectedDeviceMode}
                disabled={transcribing}
                class="bg-slate-950 border border-slate-800 text-indigo-300 font-semibold px-2 py-1.5 rounded focus:outline-none focus:border-indigo-500 disabled:opacity-50 transition-all cursor-pointer text-xs"
              >
                <option value="cuda">GPU Only (Default)</option>
                <option value="auto">Auto (GPU/CPU)</option>
                <option value="cpu">CPU Only</option>
              </select>
            </div>
          </div>

          <!-- Send To Integration Panel -->
          <div class="space-y-4 border-t border-slate-900 pt-6">
            <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">Windows Explorer "Send To" Integration</h3>

            <!-- Explanation block -->
            <div class="text-xs text-slate-300 leading-relaxed bg-slate-950/40 p-4 border border-slate-900 rounded-2xl">
              <span class="font-bold text-slate-200 block mb-1">What is Windows "Send To" Integration?</span>
              Adding VoidTranscribe to the "Send To" menu lets you right-click any video file in Windows Explorer and choose <strong class="text-indigo-400 font-semibold">Send to -> VoidTranscribe</strong>.
              This immediately launches the VoidTranscribe GUI, loads the video file, and automatically starts transcribing using your configured Settings. This is a secure, clean alternative that does not require administrator privileges!
            </div>

            <!-- Status indicator -->
            <div class="p-5 rounded-2xl border flex items-center justify-between {requirements.isRegistered ? 'bg-emerald-950/20 border-emerald-900/60' : 'bg-slate-950/40 border-slate-900'} shadow-md">
              <div>
                <span class="text-[10px] uppercase font-bold text-slate-500 tracking-wider block">Send To Shortcut Status</span>
                <span class="text-sm font-bold {requirements.isRegistered ? 'text-emerald-400' : 'text-slate-300'}">
                  {requirements.isRegistered ? 'Active (Shortcut Installed)' : 'Not Installed'}
                </span>
              </div>

              <div class="h-3.5 w-3.5 rounded-full {requirements.isRegistered ? 'bg-emerald-500 shadow-md shadow-emerald-500/20' : 'bg-slate-700'}"></div>
            </div>

            <!-- Registration success/error notices -->
            {#if registryMessage}
              <div class="p-3.5 rounded-xl bg-emerald-950/30 border border-emerald-900/50 text-emerald-300 text-xs flex items-center space-x-2">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-emerald-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span>{registryMessage}</span>
              </div>
            {/if}

            {#if registryError}
              <div class="p-3.5 rounded-xl bg-red-950/30 border border-red-900/50 text-red-300 text-xs flex items-start space-x-2.5">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-red-400 shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
                <div>
                  <span class="font-bold block mb-0.5">Action Failed</span>
                  {registryError}
                </div>
              </div>
            {/if}

            <!-- Controls -->
            <div class="flex space-x-4">
              <button
                on:click={handleRegisterRegistry}
                disabled={requirements.isRegistered}
                class="flex-1 py-3 rounded-xl bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 font-bold text-xs tracking-wider text-white shadow-md shadow-emerald-700/10 disabled:opacity-40 disabled:pointer-events-none transition-all cursor-pointer"
              >
                Add SendTo Shortcut
              </button>
              <button
                on:click={handleUnregisterRegistry}
                disabled={!requirements.isRegistered}
                class="px-6 py-3 rounded-xl bg-slate-800 hover:bg-red-950/40 text-slate-300 hover:text-red-400 font-bold text-xs tracking-wider border border-slate-700/60 hover:border-red-900/40 transition-all cursor-pointer"
              >
                Remove Shortcut
              </button>
            </div>
          </div>
        </div>
      </div>
    {/if}

  </main>

  <!-- Sleek Mini Footer -->
  <footer class="h-9 border-t border-slate-900 bg-slate-950/20 px-8 flex items-center justify-between text-[10px] text-slate-500 shrink-0">
    <span>Transcribe a video offline</span>
    <span class="flex items-center space-x-3">
      <span>Model: {selectedModel}</span>
      <span class="h-1 w-1 bg-slate-700 rounded-full"></span>
      <span>By VoidBean</span>
    </span>
  </footer>

  <!-- CUDA Missing Installation Overlay Prompt Modal -->
  {#if showCudaPrompt}
    <div class="fixed inset-0 bg-slate-950/85 backdrop-blur-md flex items-center justify-center z-50 p-4 select-none">
      <div class="w-full max-w-lg bg-slate-900 border border-slate-800/80 rounded-3xl p-6 shadow-2xl flex flex-col space-y-4 text-left relative overflow-hidden">

        {#if cudaInstalling}
          <!-- Installation Panel -->
          <div class="flex items-center space-x-3">
            <div class="h-9 w-9 rounded-xl bg-indigo-600/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400 shrink-0">
              <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div>
              <h3 class="text-sm font-bold text-indigo-400 animate-pulse">Installing CUDA Libraries...</h3>
              <p class="text-[10px] text-slate-500">Downloading nvidia-cublas-cu12 and nvidia-cudnn-cu12. Please do not close the app.</p>
            </div>
          </div>

          <div class="h-60 rounded-xl bg-slate-950 border border-slate-900 p-3.5 flex flex-col min-h-0 shrink-0">
            <div
              bind:this={cudaConsoleElement}
              class="flex-1 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-1.5 pr-2 custom-scrollbar text-left select-all"
            >
              {#each cudaInstallLogs as log}
                {#if log.includes("[ERROR]")}
                  <div class="text-red-400 font-bold">{log}</div>
                {:else if log.includes("[WARN]")}
                  <div class="text-amber-400">{log}</div>
                {:else if log.includes("[GUI]")}
                  <div class="text-teal-400">{log}</div>
                {:else}
                  <div>{log}</div>
                {/if}
              {/each}
            </div>
          </div>
        {:else if cudaInstallComplete}
          <!-- Completion Panel -->
          <div class="flex items-start space-x-4">
            {#if cudaInstallSuccess}
              <div class="h-12 w-12 rounded-2xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400 shrink-0 mt-0.5 animate-bounce">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              </div>
              <div class="space-y-1 min-w-0 flex-1">
                <h3 class="text-base font-bold text-slate-100">CUDA Libraries Installed!</h3>
                <p class="text-xs text-slate-400 leading-relaxed">
                  All required Nvidia CUDA 12 DLLs were successfully installed and registered. GPU-accelerated local transcription is now fully operational!
                </p>
              </div>
            {:else}
              <div class="h-12 w-12 rounded-2xl bg-red-500/10 border border-red-500/20 flex items-center justify-center text-red-400 shrink-0 mt-0.5">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
              </div>
              <div class="space-y-1 min-w-0 flex-1">
                <h3 class="text-base font-bold text-slate-100">Installation Failed</h3>
                <p class="text-xs text-slate-400 leading-relaxed">
                  The installer encountered an error during CUDA library provisioning. Please see logs below for details.
                 </p>
              </div>
            {/if}
          </div>

          <!-- Mini Log View in completed screen -->
          <div class="h-44 rounded-xl bg-slate-950 border border-slate-900/60 p-3.5 flex flex-col min-h-0 shrink-0">
            <div class="text-[9px] font-bold text-slate-500 uppercase tracking-wider pb-1.5 border-b border-slate-900 mb-1.5 flex justify-between items-center shrink-0">
              <span>Installation Logs</span>
              <span class="text-slate-600 font-mono text-[8px]">{cudaInstallLogs.length} lines</span>
            </div>
            <div
              bind:this={cudaConsoleElement}
              class="flex-1 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-1.5 pr-2 custom-scrollbar text-left select-all"
            >
              {#each cudaInstallLogs as log}
                {#if log.includes("[ERROR]")}
                  <div class="text-red-400 font-bold">{log}</div>
                {:else if log.includes("[WARN]")}
                  <div class="text-amber-400">{log}</div>
                {:else if log.includes("[GUI]")}
                  <div class="text-teal-400">{log}</div>
                {:else}
                  <div>{log}</div>
                {/if}
              {/each}
            </div>
          </div>

          <div class="flex space-x-3 pt-2">
            {#if cudaInstallSuccess}
              <button
                on:click={() => showCudaPrompt = false}
                class="flex-1 py-2.5 rounded-xl bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 text-white font-bold text-xs tracking-wider transition-all active:scale-98 shadow-lg shadow-emerald-600/10"
              >
                Continue to Transcription
              </button>
            {:else}
              <button
                on:click={startCudaInstallation}
                class="flex-1 py-2.5 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white font-bold text-xs tracking-wider transition-all active:scale-98 shadow-lg shadow-indigo-600/10"
              >
                Retry Installation
              </button>
              <button
                on:click={() => { selectedDeviceMode = 'cpu'; showCudaPrompt = false; }}
                class="px-4 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-300 font-semibold text-xs tracking-wider border border-slate-700/60 transition-all"
              >
                Use CPU Mode
              </button>
            {/if}
          </div>
        {:else}
          <!-- Prompt Panel -->
          <div class="flex items-start space-x-4">
            <div class="h-12 w-12 rounded-2xl bg-amber-500/10 border border-amber-500/20 flex items-center justify-center text-amber-400 shrink-0 mt-0.5">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
            </div>
            <div class="space-y-1 min-w-0">
              <h3 class="text-base font-bold text-slate-100">Local CUDA DLLs Required</h3>
              <p class="text-xs text-slate-400 leading-relaxed">
                GPU-accelerated local transcription requires the Nvidia CUDA 12 execution libraries (<code class="text-indigo-400 font-semibold font-mono">cublas64_12.dll</code>, etc.). These DLL files were not found in your portable Python environment.
              </p>
            </div>
          </div>

          <div class="text-[11px] text-slate-300 leading-relaxed bg-slate-950/45 p-4 rounded-2xl border border-slate-900/60">
            <span class="font-bold block text-slate-400 mb-1">Zero-Config Portable Solution:</span>
            VoidTranscribe can download and install these CUDA support DLLs directly into your isolated Python folder. This will enable native GPU hardware acceleration without changing your global Windows system path or settings (Approx. 1.2GB download).
          </div>

          <div class="flex space-x-3 pt-2">
            <button
              on:click={startCudaInstallation}
              class="flex-1 py-2.5 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white font-bold text-xs tracking-wider transition-all active:scale-98 shadow-lg shadow-indigo-600/10"
            >
              Download & Install CUDA
            </button>
            <button
              on:click={() => { selectedDeviceMode = 'cpu'; showCudaPrompt = false; }}
              class="px-4 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-300 font-semibold text-xs tracking-wider border border-slate-700/60 transition-all"
            >
              Use CPU Mode
            </button>
            <button
              on:click={() => showCudaPrompt = false}
              class="px-3.5 py-2.5 rounded-xl hover:bg-slate-800 text-slate-400 font-semibold text-xs transition-all"
            >
              Cancel
            </button>
          </div>
        {/if}

      </div>
    </div>
  {/if}

  <!-- VRAM WARNING MODAL -->
  {#if showVramWarning}
    <div class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-md p-4 transition-all duration-300">
      <div class="w-full max-w-md rounded-2xl bg-slate-900/95 border border-slate-800 p-6 flex flex-col space-y-4 shadow-2xl relative animate-fade-in">
        <div class="flex items-start space-x-3.5">
          <div class="h-12 w-12 rounded-2xl bg-amber-500/10 border border-amber-500/20 flex items-center justify-center text-amber-400 shrink-0 mt-0.5 shadow-md shadow-amber-500/5">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          </div>
          <div class="space-y-1 min-w-0">
            <h3 class="text-base font-bold text-slate-100">GPU VRAM Limit Reached</h3>
            <p class="text-xs text-slate-400 leading-relaxed">
              The selected model <span class="text-indigo-400 font-bold font-mono">{selectedModel}</span> recommends at least <span class="text-indigo-300 font-semibold">{requiredVramGB.toFixed(1)} GB</span> of GPU VRAM capacity to load and run stably.
            </p>
          </div>
        </div>

        <div class="text-[11px] text-slate-350 leading-relaxed bg-slate-950/45 p-4 rounded-2xl border border-slate-900/60 space-y-2">
          <p>
            Your primary graphics card only reports <span class="text-amber-400 font-bold">{detectedVramGB.toFixed(2)} GB</span> of total memory.
          </p>
          <p class="text-slate-400">
            Continuing with this configuration may lead to <span class="font-semibold text-slate-300">CUDA Out-of-Memory (OOM)</span> errors, severe system lagging, or process crashes. We recommend using CPU execution or choosing a lighter model size.
          </p>
        </div>

        <div class="flex flex-col space-y-2.5 pt-1">
          <button
            on:click={bypassVramAndRun}
            class="w-full py-2.5 rounded-xl bg-amber-600 hover:bg-amber-500 text-white font-bold text-xs tracking-wider transition-all active:scale-98 shadow-lg shadow-amber-600/10"
          >
            Continue Anyways (Force GPU)
          </button>
          <button
            on:click={() => { selectedDeviceMode = 'cpu'; showVramWarning = false; startTranscription(); }}
            class="w-full py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-200 font-bold text-xs tracking-wider transition-all active:scale-98 border border-slate-700/40"
          >
            Switch to CPU Mode & Run
          </button>
          <button
            on:click={() => showVramWarning = false}
            class="w-full py-2.5 rounded-xl bg-slate-950 border border-slate-900 hover:bg-slate-900 text-slate-400 font-semibold text-xs tracking-wider transition-all active:scale-98"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  /* Custom scrollbar formatting for slick dark theme */
  .custom-scrollbar::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  .custom-scrollbar::-webkit-scrollbar-track {
    background: transparent;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb {
    background: #1e293b;
    border-radius: 9999px;
  }
  .custom-scrollbar::-webkit-scrollbar-thumb:hover {
    background: #312e81;
  }
</style>
