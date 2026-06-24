<script>
  import { onMount, onDestroy } from 'svelte';
  import {
    CheckRequirements,
    TranscribeVideo,
    SelectVideoFileDialog,
    CancelTranscription,
    InstallCudaLibraries,
    OpenFile,
    OpenFolder,
    SelectTextFileDialog,
    GetVersion,
    GetGpuVramGB,
    GetStartupVideoPath,
    RegisterSendTo,
    UnregisterSendTo,
    ValidateVideoFile,
    InstallPortableEnvironment,
    CancelEnvironmentInstallation,
    LoadConfig,
    SaveConfig
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

  // Environment installer state
  let envInstalling = false;
  let envInstallComplete = false;
  let envInstallSuccess = false;
  let envInstallLogs = [];
  let showEnvPrompt = false;
  let envConsoleElement;
  let envInstallProgress = 0;
  let envInstallStatusText = "";

  // Transcription State
  let selectedVideoPath = "";
  let transcribing = false;
  let progress = 0;
  let statusMessage = "Select a video to get started";
  let transcriptionText = "";
  let selectedDeviceMode = "cuda"; // 'cuda' = GPU Only, 'auto' = Auto, 'cpu' = CPU Only
  let selectedTimecodeFormat = "davinci";
  let prePromptFilePath = "";
  let appVersion = "1.0.0";
  let queueStartTime = null;
  let queueElapsedSeconds = 0;
  let queueTimerInterval = null;

  let selectedModel = "distil-medium.en";
  let showVramWarning = false;
  let detectedVramGB = 0;
  let requiredVramGB = 0;

  let userScrolledUp = false;
  let logsScrolledUp = false;

  // Queue & Drag-drop State
  let queue = [];
  let selectedQueueIndex = -1;
  let activeQueueIndex = -1;
  let queueProcessing = false;
  let configLoaded = false;

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
  let duration = "";

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

  $: selectedItem = queue[selectedQueueIndex] || null;

  $: globalProgress = (() => {
    if (queue.length === 0) return 0;
    const totalProgressSum = queue.reduce((sum, item) => {
      if (item.status === 'completed') return sum + 100;
      if (item.status === 'transcribing') return sum + item.progress;
      return sum;
    }, 0);
    return totalProgressSum / queue.length;
  })();

  onMount(async () => {
    // Prevent default drag and drop behavior globally to stop the browser from opening dropped files
    window.addEventListener("dragover", preventDefaultBehavior, false);
    window.addEventListener("drop", preventDefaultBehavior, false);

    await runCheckRequirements();

    try {
      const config = await LoadConfig();
      selectedTimecodeFormat = config.timecodeFormat || "davinci";
      selectedModel = config.selectedModel || "distil-medium.en";
      selectedDeviceMode = config.selectedDeviceMode || "cuda";
      prePromptFilePath = config.prePromptFilePath || "";
    } catch (err) {
      console.error("Failed to load config file, falling back to localStorage:", err);
      selectedTimecodeFormat = localStorage.getItem('timecodeFormat') || "davinci";
      selectedModel = localStorage.getItem('selectedModel') || "distil-medium.en";
      selectedDeviceMode = localStorage.getItem('selectedDeviceMode') || "cuda";
      prePromptFilePath = localStorage.getItem('prePromptFilePath') || "";
    } finally {
      configLoaded = true;
    }

    try {
      appVersion = await GetVersion();
    } catch (err) {
      console.error("Failed to get version:", err);
    }

    // Register Wails Event Listeners for stdout/stderr streaming
    EventsOn("transcription-stdout", handleStdout);
    EventsOn("transcription-stderr", handleStderr);
    EventsOn("cuda-install-log", handleCudaInstallLog);
    EventsOn("cuda-install-complete", handleCudaInstallComplete);
    EventsOn("env-install-log", handleEnvInstallLog);
    EventsOn("env-install-complete", handleEnvInstallComplete);

    // Check for startup video path passed via command line (Explorer context menu)
    try {
      const startupPath = await GetStartupVideoPath();
      if (startupPath) {
        addToQueue([startupPath]);
      }
    } catch (err) {
      console.error("Failed to retrieve startup video path:", err);
    }
  });

  onDestroy(() => {
    window.removeEventListener("dragover", preventDefaultBehavior, false);
    window.removeEventListener("drop", preventDefaultBehavior, false);

    EventsOff("transcription-stdout");
    EventsOff("transcription-stderr");
    EventsOff("cuda-install-log");
    EventsOff("cuda-install-complete");
    EventsOff("env-install-log");
    EventsOff("env-install-complete");
  });

  // Auto-scroll logs and segments when updated
  $: if (transcribing && !logsScrolledUp && realtimeLogs && logConsoleElement) {
    setTimeout(() => {
      if (logConsoleElement) {
        logConsoleElement.scrollTop = logConsoleElement.scrollHeight;
      }
    }, 50);
  }

  async function saveConfig() {
    if (!configLoaded) return;
    try {
      await SaveConfig({
        timecodeFormat: selectedTimecodeFormat,
        selectedModel: selectedModel,
        selectedDeviceMode: selectedDeviceMode,
        prePromptFilePath: prePromptFilePath
      });
      // Also sync to localStorage as a webview backup
      localStorage.setItem('timecodeFormat', selectedTimecodeFormat);
      localStorage.setItem('selectedModel', selectedModel);
      localStorage.setItem('selectedDeviceMode', selectedDeviceMode);
      localStorage.setItem('prePromptFilePath', prePromptFilePath);
    } catch (err) {
      console.error("Failed to save config file:", err);
    }
  }

  $: if (selectedTimecodeFormat || selectedModel || selectedDeviceMode || prePromptFilePath !== undefined) {
    saveConfig();
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

  $: if (envInstallLogs && envConsoleElement) {
    setTimeout(() => {
      if (envConsoleElement) {
        envConsoleElement.scrollTop = envConsoleElement.scrollHeight;
      }
    }, 50);
  }

  async function runCheckRequirements() {
    checkingRequirements = true;
    requirementsError = "";
    try {
      requirements = await CheckRequirements();
      if (!requirements.pythonExists || !requirements.ffmpegExists || !requirements.fasterWhisperReady) {
        requirementsError = "Incomplete portable environment setup. Please click 'Setup Portable Environment' below to install Python, FFmpeg, and dependencies.";
      }
    } catch (err) {
      console.error(err);
      requirementsError = "Failed to communicate with the Go backend: " + err.message;
    } finally {
      checkingRequirements = false;
    }
  }

  function handleStdout(line) {
    const durationMatch = line.match(/duration\s+([0-9:.]+)/i);
    if (durationMatch) {
      let dur = durationMatch[1];
      if (dur.includes(":")) {
        if (dur.includes(".")) {
          dur = dur.split(".")[0];
        }
        const parts = dur.split(":");
        if (parts.length === 2) {
          dur = "00:" + dur;
        }
      } else {
        const secs = Math.round(parseFloat(dur));
        if (!isNaN(secs)) {
          const hrs = Math.floor(secs / 3600);
          const mins = Math.floor((secs % 3600) / 60);
          const s = secs % 60;
          const pad = (num) => String(num).padStart(2, '0');
          dur = `${pad(hrs)}:${pad(mins)}:${pad(s)}`;
        }
      }
      if (activeQueueIndex !== -1) {
        updateQueueItem(activeQueueIndex, { duration: dur });
      }
      if (selectedQueueIndex === activeQueueIndex) {
        duration = dur;
      }
    }

    // Check for special prefix tags from transcribe.py
    if (line.startsWith("[PROGRESS] ")) {
      const pct = parseFloat(line.substring(11).trim());
      if (!isNaN(pct)) {
        if (activeQueueIndex !== -1) {
          updateQueueItem(activeQueueIndex, { progress: pct });
        }
        if (selectedQueueIndex === activeQueueIndex) {
          progress = pct;
        }
      }
    } else if (line.startsWith("[SEGMENT] ")) {
      const segmentStr = line.substring(10).trim();
      const timecodeEndIdx = segmentStr.indexOf("]");
      let newSegment;
      if (timecodeEndIdx > 0) {
        const timecode = segmentStr.substring(0, timecodeEndIdx + 1);
        const text = segmentStr.substring(timecodeEndIdx + 1).trim();
        newSegment = { timecode, text };
      } else {
        newSegment = { timecode: "", text: segmentStr };
      }
      if (activeQueueIndex !== -1) {
        const currentSegments = queue[activeQueueIndex].segments || [];
        updateQueueItem(activeQueueIndex, { segments: [...currentSegments, newSegment] });
      }
      if (selectedQueueIndex === activeQueueIndex) {
        realtimeSegments = [...realtimeSegments, newSegment];
      }
    } else if (line.startsWith("[LOG] ")) {
      const msg = line.substring(6).trim();
      const logLine = `[INFO] ${msg}`;

      let stageUpdate = {};
      if (msg.includes("Stage 1/3:")) {
        stageUpdate = { stage: 1 };
      } else if (msg.includes("Stage 2/3:")) {
        stageUpdate = { stage: 2 };
      } else if (msg.includes("Stage 3/3:")) {
        stageUpdate = { stage: 3 };
      }

      if (activeQueueIndex !== -1) {
        const currentLogs = queue[activeQueueIndex].logs || [];
        updateQueueItem(activeQueueIndex, { logs: [...currentLogs, logLine], ...stageUpdate });
      }
      if (selectedQueueIndex === activeQueueIndex) {
        realtimeLogs = [...realtimeLogs, logLine];
        if (stageUpdate.stage !== undefined) {
          currentStage = stageUpdate.stage;
        }
      }
    } else {
      if (activeQueueIndex !== -1) {
        const currentLogs = queue[activeQueueIndex].logs || [];
        updateQueueItem(activeQueueIndex, { logs: [...currentLogs, line] });
      }
      if (selectedQueueIndex === activeQueueIndex) {
        realtimeLogs = [...realtimeLogs, line];
      }
    }
  }

  function handleStderr(line) {
    if (line.trim()) {
      const durationMatch = line.match(/duration\s+([0-9:.]+)/i);
      if (durationMatch) {
        let dur = durationMatch[1];
        if (dur.includes(":")) {
          if (dur.includes(".")) {
            dur = dur.split(".")[0];
          }
          const parts = dur.split(":");
          if (parts.length === 2) {
            dur = "00:" + dur;
          }
        } else {
          const secs = Math.round(parseFloat(dur));
          if (!isNaN(secs)) {
            const hrs = Math.floor(secs / 3600);
            const mins = Math.floor((secs % 3600) / 60);
            const s = secs % 60;
            const pad = (num) => String(num).padStart(2, '0');
            dur = `${pad(hrs)}:${pad(mins)}:${pad(s)}`;
          }
        }
        if (activeQueueIndex !== -1) {
          updateQueueItem(activeQueueIndex, { duration: dur });
        }
        if (selectedQueueIndex === activeQueueIndex) {
          duration = dur;
        }
      }

      const logLine = `[WARN] ${line}`;
      if (activeQueueIndex !== -1) {
        const currentLogs = queue[activeQueueIndex].logs || [];
        updateQueueItem(activeQueueIndex, { logs: [...currentLogs, logLine] });
      }
      if (selectedQueueIndex === activeQueueIndex) {
        realtimeLogs = [...realtimeLogs, logLine];
      }
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

  function handleEnvInstallLog(line) {
    const cleanLine = line.replace("[WARN]", "").replace("[ERROR]", "").trim();
    if (cleanLine.startsWith("[PROGRESS]")) {
      const parts = cleanLine.split("|");
      const pctStr = parts[0].replace("[PROGRESS]", "").trim();
      const pct = parseInt(pctStr, 10);
      if (!isNaN(pct)) {
        envInstallProgress = pct;
        if (parts[1]) {
          envInstallStatusText = parts[1].trim();
        }
      }
      return;
    }
    envInstallLogs = [...envInstallLogs, line];
  }

  async function handleEnvInstallComplete() {
    envInstalling = false;
    envInstallComplete = true;
    envInstallSuccess = true;
    envInstallProgress = 100;
    await runCheckRequirements(); // Refresh requirements state
  }

  async function startEnvInstallation() {
    envInstalling = true;
    envInstallComplete = false;
    envInstallSuccess = false;
    envInstallProgress = 0;
    envInstallStatusText = "Initiating setup...";
    envInstallLogs = ["[GUI] Initiating portable environment setup..."];
    try {
      await InstallPortableEnvironment();
      envInstallComplete = true;
      envInstallSuccess = true;
      await runCheckRequirements();
    } catch (err) {
      console.error(err);
      envInstallLogs = [...envInstallLogs, `[ERROR] Installation failed: ${err.message || err}`];
      envInstallComplete = true;
      envInstallSuccess = false;
    } finally {
      envInstalling = false;
    }
  }

  async function cancelEnvInstallation() {
    try {
      envInstallLogs = [...envInstallLogs, "[GUI] Requesting cancellation..."];
      await CancelEnvironmentInstallation();
    } catch (err) {
      console.error(err);
      envInstallLogs = [...envInstallLogs, `[ERROR] Cancellation failed: ${err.message || err}`];
    }
  }

  async function browseVideo() {
    try {
      const result = await SelectVideoFileDialog();
      if (result && result.length > 0) {
        addToQueue(result);
      }
    } catch (err) {
      statusMessage = "Error selecting file: " + err;
    }
  }

  async function browsePrePrompt() {
    try {
      const result = await SelectTextFileDialog();
      if (result) {
        prePromptFilePath = result;
      }
    } catch (err) {
      console.error("Error selecting pre-prompt file:", err);
    }
  }

  function clearPrePrompt() {
    prePromptFilePath = "";
  }


  function addToQueue(paths) {
    const newItems = [];
    paths.forEach(path => {
      const isDuplicate = queue.some(item => item.path === path && (item.status === 'pending' || item.status === 'transcribing'));
      if (!isDuplicate) {
        newItems.push({
          path,
          name: getFileName(path),
          status: 'pending',
          progress: 0,
          statusMessage: 'Queued',
          text: '',
          logs: [],
          segments: [],
          elapsedSeconds: 0,
          error: '',
          stage: 0,
          duration: ''
        });
      }
    });

    if (newItems.length > 0) {
      queue = [...queue, ...newItems];

      // If nothing is selected, select the first of the newly added items
      if (selectedQueueIndex === -1) {
        selectQueueItem(queue.length - newItems.length);
      }


    }
  }

  function removeFromQueue(index) {
    if (index === activeQueueIndex) return; // Prevent removing currently transcribing item

    const wasSelected = selectedQueueIndex === index;
    queue = queue.filter((_, i) => i !== index);

    // Adjust activeQueueIndex
    if (activeQueueIndex > index) {
      activeQueueIndex--;
    }

    // Adjust selectedQueueIndex
    if (wasSelected) {
      if (queue.length > 0) {
        selectQueueItem(Math.max(0, index - 1));
      } else {
        selectedQueueIndex = -1;
        selectedVideoPath = "";
        realtimeLogs = [];
        realtimeSegments = [];
        progress = 0;
        statusMessage = "Select a video to get started";
        transcriptionText = "";
      }
    } else if (selectedQueueIndex > index) {
      selectedQueueIndex--;
    }
  }

  function clearCompletedFromQueue() {
    queue = queue.filter(item => item.status === 'pending' || item.status === 'transcribing');

    // Re-evaluate selected item index
    let found = -1;
    for (let i = 0; i < queue.length; i++) {
      if (queue[i].path === selectedVideoPath) {
        found = i;
        break;
      }
    }

    if (found !== -1) {
      selectedQueueIndex = found;
    } else {
      if (queue.length > 0) {
        selectQueueItem(0);
      } else {
        selectedQueueIndex = -1;
        selectedVideoPath = "";
        realtimeLogs = [];
        realtimeSegments = [];
        progress = 0;
        statusMessage = "Select a video to get started";
        transcriptionText = "";
      }
    }
  }

  function selectQueueItem(index) {
    selectedQueueIndex = index;
    const item = queue[index];
    if (item) {
      selectedVideoPath = item.path;
      realtimeLogs = item.logs || [];
      realtimeSegments = item.segments || [];
      progress = item.progress || 0;
      statusMessage = item.statusMessage || "";
      transcriptionText = item.text || "";
      elapsedSeconds = item.elapsedSeconds || 0;
      duration = item.duration || "";

      if (item.status === 'completed') {
        currentStage = 4;
      } else if (item.status === 'transcribing') {
        currentStage = item.stage || 1;
      } else {
        currentStage = 0;
      }
    }
  }

  function updateQueueItem(index, fields) {
    queue = queue.map((item, i) => {
      if (i === index) {
        return { ...item, ...fields };
      }
      return item;
    });
  }

  async function processQueue() {
    if (queueProcessing) return;

    // Reset cancelled and failed items back to pending so they are retried/resumed
    queue = queue.map(item => {
      if (item.status === 'cancelled' || item.status === 'failed') {
        return { ...item, status: 'pending', progress: 0, statusMessage: 'Queued', error: '' };
      }
      return item;
    });

    queueProcessing = true;

    queueElapsedSeconds = 0;
    queueStartTime = Date.now();
    if (queueTimerInterval) clearInterval(queueTimerInterval);
    queueTimerInterval = setInterval(() => {
      queueElapsedSeconds = Math.floor((Date.now() - queueStartTime) / 1000);
    }, 1000);

    try {
      while (queueProcessing) {
        const nextIdx = queue.findIndex(item => item.status === 'pending');
        if (nextIdx === -1) {
          break;
        }

        activeQueueIndex = nextIdx;
        const currentItem = queue[nextIdx];

        updateQueueItem(nextIdx, { status: 'transcribing', progress: 0, statusMessage: 'Initializing...' });

        selectQueueItem(nextIdx);

        await runSingleTranscription(nextIdx);

        if (queue[nextIdx].status === 'cancelled' || !queueProcessing) {
          break;
        }
      }
    } finally {
      queueProcessing = false;
      activeQueueIndex = -1;
      if (queueTimerInterval) {
        clearInterval(queueTimerInterval);
        queueTimerInterval = null;
      }
    }
  }

  async function runSingleTranscription(index) {
    const item = queue[index];
    const path = item.path;

    try {
      updateQueueItem(index, { statusMessage: "Verifying file format..." });
      if (selectedQueueIndex === index) {
        statusMessage = "Verifying file format...";
      }

      const validation = await ValidateVideoFile(path);
      if (!validation.isValid) {
        const errMsg = "Error: " + validation.errorMessage;
        updateQueueItem(index, {
          status: 'failed',
          statusMessage: errMsg,
          error: validation.errorMessage
        });
        if (selectedQueueIndex === index) {
          statusMessage = errMsg;
        }
        return;
      }
      if (!validation.hasAudio) {
        const errMsg = "Error: " + validation.errorMessage;
        updateQueueItem(index, {
          status: 'failed',
          statusMessage: errMsg,
          error: validation.errorMessage
        });
        if (selectedQueueIndex === index) {
          statusMessage = errMsg;
        }
        return;
      }
    } catch (err) {
      console.error("File format validation failed:", err);
    }

    if (selectedDeviceMode === 'cuda' && !requirements.cudaLibsExists) {
      showCudaPrompt = true;
      const errMsg = "Error: CUDA DLLs missing";
      updateQueueItem(index, {
        status: 'failed',
        statusMessage: errMsg,
        error: "CUDA DLLs missing"
      });
      if (selectedQueueIndex === index) {
        statusMessage = errMsg;
      }
      return;
    }

    if (selectedDeviceMode !== 'cpu') {
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
        if (vramGB > 0 && vramGB < (req - 0.2)) {
          showVramWarning = true;
          while (showVramWarning) {
            await new Promise(resolve => setTimeout(resolve, 200));
          }
        }
      } catch (err) {
        console.error("VRAM query failed:", err);
      }
    }

    transcribing = true;
    if (selectedQueueIndex === index) {
      progress = 0;
      currentStage = 1;
      transcriptionText = "";
      realtimeLogs = ["[GO] Starting Python runner process..."];
      realtimeSegments = [];
      statusMessage = "Initializing Whisper engine...";
      duration = "";
    }
    updateQueueItem(index, {
      progress: 0,
      stage: 1,
      logs: ["[GO] Starting Python runner process..."],
      segments: []
    });

    userScrolledUp = false;
    logsScrolledUp = false;

    elapsedSeconds = 0;
    startTime = Date.now();
    if (timerInterval) clearInterval(timerInterval);
    timerInterval = setInterval(() => {
      elapsedSeconds = Math.floor((Date.now() - startTime) / 1000);
      updateQueueItem(index, { elapsedSeconds });
    }, 1000);

    try {
      const result = await TranscribeVideo(path, selectedDeviceMode, selectedTimecodeFormat, selectedModel, prePromptFilePath);

      if (selectedQueueIndex === index) {
        transcriptionText = result;
        statusMessage = "Transcription completed successfully!";
        progress = 100;
        currentStage = 4;
      }

      updateQueueItem(index, {
        status: 'completed',
        progress: 100,
        text: result,
        statusMessage: "Completed"
      });
    } catch (err) {
      console.error(err);
      if (selectedQueueIndex === index) {
        currentStage = 0;
      }

      const errStr = (err && err.message) || String(err) || "";
      let isCancelled = errStr.toLowerCase().includes("cancelled") || !queueProcessing;
      const finalStatus = isCancelled ? 'cancelled' : 'failed';
      const finalMsg = isCancelled ? 'Cancelled' : ('Failed: ' + err);

      updateQueueItem(index, {
        status: finalStatus,
        error: err.message || err,
        statusMessage: finalMsg,
        stage: 0
      });

      if (selectedQueueIndex === index) {
        if (isCancelled) {
          statusMessage = "Transcription cancelled.";
        } else {
          statusMessage = "Transcription failed: " + err;
          realtimeLogs = [...realtimeLogs, `[ERROR] Go Backend: ${err}`];
        }
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
  }

  async function cancelTranscriptionProcess() {
    try {
      statusMessage = "Cancelling transcription...";
      realtimeLogs = [...realtimeLogs, "[GO] Requesting process cancellation..."];
      queueProcessing = false;
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

  async function openTranscriptFolder() {
    if (!selectedVideoPath) return;
    const idx = Math.max(selectedVideoPath.lastIndexOf('\\'), selectedVideoPath.lastIndexOf('/'));
    if (idx === -1) return;
    const dirPath = selectedVideoPath.substring(0, idx);
    try {
      await OpenFolder(dirPath);
    } catch (err) {
      console.error(err);
      realtimeLogs = [...realtimeLogs, `[ERROR] Failed to open transcript folder: ${err.message || err}`];
    }
  }

  async function handleRegisterRegistry() {
    registryMessage = "";
    registryError = "";
    try {
      await RegisterSendTo();
      registryMessage = "SendTo shortcut successfully created!";
      await runCheckRequirements();
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
      await runCheckRequirements();
    } catch (err) {
      console.error(err);
      registryError = err.message || "Failed to delete SendTo shortcut.";
    }
  }

  function getFileName(path) {
    if (!path) return "";
    return path.split('\\').pop().split('/').pop();
  }

  function preventDefaultBehavior(e) {
    if (e) {
      e.preventDefault();
    }
  }
</script>

<div
  class="h-screen w-screen bg-slate-950 text-slate-100 flex flex-col font-sans overflow-hidden select-none"
  style="background: radial-gradient(circle at 80% 20%, rgba(99, 102, 241, 0.15), transparent), radial-gradient(circle at 10% 80%, rgba(168, 85, 247, 0.12), transparent), #090d16;"
>

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
        <span class="text-xs px-2.5 py-1 rounded-full bg-slate-900/80 text-slate-350 border border-slate-800/80 flex items-center space-x-1.5 shadow-md">
          <span class="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
          <span>Whisper Engine Ready</span>
        </span>
      {:else}
        <span class="text-xs px-2.5 py-1 rounded-full bg-red-950/80 text-red-400 border border-red-900/60 flex items-center space-x-1.5 shadow-md shadow-red-950/10">
          <span class="h-1.5 w-1.5 rounded-full bg-red-400"></span>
          <span>Engine Offline</span>
        </span>
      {/if}

      <!-- Tab Buttons -->
      <nav class="flex p-1 rounded-xl bg-slate-950/80 border border-slate-700/60 shadow-lg shadow-black/25">
        <button
          on:click={() => activeTab = 'transcribe'}
          class="flex items-center space-x-1.5 px-4 py-1.5 rounded-lg text-xs font-bold transition-all duration-250 cursor-pointer
            {activeTab === 'transcribe'
              ? 'bg-gradient-to-r from-indigo-600 to-purple-600 text-white shadow-md shadow-indigo-600/30 border border-indigo-500/25'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-900/30'}"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M19 11a7 7 0 01-7 7m0 0a7 7 0 01-7-7m7 7v4m0 0H8m4 0h4m-4-8a3 3 0 01-3-3V5a3 3 0 116 0v6a3 3 0 01-3 3z" />
          </svg>
          <span>Transcribe</span>
        </button>
        <button
          on:click={() => activeTab = 'settings'}
          class="flex items-center space-x-1.5 px-4 py-1.5 rounded-lg text-xs font-bold transition-all duration-250 cursor-pointer
            {activeTab === 'settings'
              ? 'bg-gradient-to-r from-indigo-600 to-purple-600 text-white shadow-md shadow-indigo-600/30 border border-indigo-500/25'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-900/30'}"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
            <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
          <span>Settings</span>
        </button>
      </nav>
    </div>
  </header>

  <!-- Main Display Body -->
  <main class="flex-1 p-8 overflow-hidden flex flex-col items-center justify-center">

    <!-- Tab 1: Transcription Studio -->
    {#if activeTab === 'transcribe'}
      <div class="w-full h-full flex flex-col space-y-6">

        <!-- Requirements Alert banner -->
        {#if !checkingRequirements && requirementsError}
          <div class="px-5 py-3.5 rounded-xl bg-amber-950/30 border border-amber-900/50 text-amber-300 text-xs flex items-start space-x-3 shadow-lg">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-amber-400 shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <div class="flex-1 mr-4">
              <span class="font-bold block mb-0.5">Portable dependencies missing</span>
              {requirementsError}
            </div>
            <div class="flex items-center space-x-2 shrink-0 self-center">
              <button
                on:click={() => showEnvPrompt = true}
                class="px-3 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white font-bold border border-indigo-500/30 transition-all shadow shadow-indigo-600/20 cursor-pointer active:scale-98"
              >
                Auto-Install Setup
              </button>
              <button
                on:click={runCheckRequirements}
                class="px-3 py-1.5 rounded-lg bg-amber-500/20 hover:bg-amber-500/30 text-amber-200 font-semibold border border-amber-500/30 transition-all cursor-pointer active:scale-98"
              >
                Retry Check
              </button>
            </div>
          </div>
        {/if}

        <!-- Workspace Layout -->
        <div class="flex-1 grid grid-cols-5 gap-6 min-h-0">

          <!-- Left Part: Selector and Controls -->
          <div class="col-span-2 flex flex-col space-y-5 min-h-0">

            <!-- Compact Dropzone/Browse Area -->
            <div
              on:click={transcribing ? null : browseVideo}
              class="h-24 rounded-2xl border-2 border-dashed border-slate-800 bg-slate-900/25 transition-all duration-300 flex items-center justify-center px-4 py-3 backdrop-blur-sm relative shrink-0
                {transcribing ? 'opacity-40 cursor-not-allowed pointer-events-none' : 'hover:bg-slate-900/40 hover:border-indigo-500/50 cursor-pointer group'}"
            >
              <div class="h-10 w-10 rounded-xl bg-slate-900/60 border border-slate-800 flex items-center justify-center text-slate-400 {transcribing ? '' : 'group-hover:text-indigo-400 group-hover:border-indigo-500/30 group-hover:scale-105'} shadow-inner transition-all duration-300 mr-3">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3m0 0v3m0-3h3m-3 0H9m12 0a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              </div>
              <div class="text-left">
                <span class="text-xs font-semibold text-slate-300 {transcribing ? '' : 'group-hover:text-slate-100'} transition-colors block">Add Media Files to Queue</span>
                <span class="text-[10px] text-slate-550 block mt-0.5">
                  {#if transcribing}
                    Adding disabled while transcribing
                  {:else}
                    Click to browse or drag-drop files anywhere
                  {/if}
                </span>
              </div>
            </div>

            <!-- Queue List Panel -->
            <div class="flex-1 rounded-2xl bg-slate-900/35 border border-slate-900 backdrop-blur-sm p-4 flex flex-col min-h-0">
              <div class="flex items-center justify-between border-b border-slate-900 pb-2 mb-3 shrink-0">
                <span class="text-xs font-bold uppercase tracking-wider text-slate-400 flex items-center space-x-1.5">
                  <span class="h-1.5 w-1.5 rounded-full bg-purple-500"></span>
                  <span>Transcription Queue ({queue.length})</span>
                </span>
                {#if queue.length > 0}
                  <button
                    on:click={clearCompletedFromQueue}
                    disabled={transcribing}
                    class="text-[9px] font-bold text-slate-400 hover:text-red-400 bg-slate-950/60 border border-slate-800/80 px-2 py-0.5 rounded transition-colors disabled:opacity-30 disabled:pointer-events-none"
                  >
                    Clear Completed
                  </button>
                {/if}
              </div>

              <!-- Queue Items -->
              <div
                on:click={queue.length === 0 ? browseVideo : null}
                class="flex-1 overflow-y-auto space-y-2 pr-1 custom-scrollbar min-h-0 {queue.length === 0 ? 'cursor-pointer hover:bg-slate-900/10 rounded-xl transition-all' : ''}"
              >
                {#if queue.length === 0}
                  <div class="h-full flex flex-col items-center justify-center text-slate-650 text-[11px] text-center py-8">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6 mb-1.5 opacity-30" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                    </svg>
                    <span>Queue is empty</span>
                    <span class="text-[9px] text-slate-500 mt-0.5">Drag & drop files or click anywhere in this area to add</span>
                  </div>
                {:else}
                  {#each queue as item, i}
                    <div
                      on:click={() => selectQueueItem(i)}
                      class="p-2.5 rounded-xl border transition-all cursor-pointer relative group flex items-center justify-between text-left
                        {selectedQueueIndex === i ? 'bg-indigo-950/20 border-indigo-500/50 shadow-md' : 'bg-slate-950/40 border-slate-900/60 hover:bg-slate-900/30'}"
                    >
                      <div class="min-w-0 flex-1 mr-2">
                        <div class="flex items-center space-x-1.5">
                          {#if item.status === 'pending'}
                            <span class="h-2 w-2 rounded-full bg-slate-650 shrink-0" title="Pending"></span>
                          {:else if item.status === 'transcribing'}
                            <span class="h-2 w-2 rounded-full bg-indigo-500 animate-pulse shrink-0" title="Processing"></span>
                          {:else if item.status === 'completed'}
                            <span class="h-2 w-2 rounded-full bg-emerald-500 shrink-0" title="Completed"></span>
                          {:else if item.status === 'failed'}
                            <span class="h-2 w-2 rounded-full bg-red-500 shrink-0" title="Failed"></span>
                          {:else if item.status === 'cancelled'}
                            <span class="h-2 w-2 rounded-full bg-amber-500 shrink-0" title="Cancelled"></span>
                          {/if}

                          <span class="text-xs font-semibold truncate block {item.status === 'transcribing' ? 'text-indigo-300' : 'text-slate-300'}" title={item.name}>
                            {item.name}
                          </span>
                        </div>
                        <span class="text-[9px] text-slate-500 block truncate mt-0.5" title={item.path}>{item.path}</span>

                        {#if item.status === 'transcribing'}
                          <div class="h-1 w-full bg-slate-900 rounded-full overflow-hidden mt-1.5">
                            <div class="h-full bg-indigo-500 rounded-full" style="width: {item.progress}%"></div>
                          </div>
                        {/if}
                      </div>

                      <div class="flex items-center space-x-1.5">
                        {#if item.status === 'transcribing'}
                          <span class="text-[10px] font-mono text-indigo-400 font-bold shrink-0">{item.progress.toFixed(0)}%</span>
                        {/if}

                        <button
                          on:click|stopPropagation={() => removeFromQueue(i)}
                          disabled={transcribing}
                          class="h-5 w-5 rounded hover:bg-red-950/40 text-slate-550 hover:text-red-400 flex items-center justify-center transition-all disabled:opacity-0 disabled:pointer-events-none"
                          title="Remove from queue"
                        >
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                          </svg>
                        </button>
                      </div>
                    </div>
                  {/each}
                {/if}
              </div>
            </div>

            <!-- Action panel -->
            <div class="p-5 rounded-2xl bg-slate-900/35 border border-slate-900 backdrop-blur-sm flex flex-col space-y-4 shrink-0">
              <!-- Output Location quick view -->
              <div class="text-[10px] text-slate-550 leading-normal space-y-1">
                <div class="flex justify-between">
                  <span>Output Location:</span>
                  <span class="text-slate-450 text-right truncate max-w-[120px]" title="Next to video file">Adjacent (.txt)</span>
                </div>
              </div>

              <!-- Main Button -->
              {#if !transcribing}
                <button
                  on:click={processQueue}
                  disabled={queue.filter(x => x.status === 'pending' || x.status === 'cancelled' || x.status === 'failed').length === 0 || requirementsError !== ""}
                  class="w-full py-3 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white font-bold text-sm tracking-wide shadow-lg shadow-indigo-600/20 disabled:opacity-40 disabled:pointer-events-none transition-all duration-300 active:scale-98 flex items-center justify-center space-x-2 cursor-pointer"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                    <path stroke-linecap="round" stroke-linejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span>
                    {#if queue.filter(x => x.status === 'pending' || x.status === 'cancelled' || x.status === 'failed').length > 1}
                      Start Queue ({queue.filter(x => x.status === 'pending' || x.status === 'cancelled' || x.status === 'failed').length} files)
                    {:else}
                      Start Transcription
                    {/if}
                  </span>
                </button>
              {:else}
                <div class="w-full flex flex-col space-y-2">
                  <div class="flex justify-between text-xs items-center">
                    <span class="font-bold text-slate-400 flex items-center">
                      <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 text-indigo-400 mr-1 animate-spin" style="animation-duration: 8s;" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      Queue Elapsed: <span class="font-mono text-indigo-300 font-bold ml-1">{formatElapsedTime(queueElapsedSeconds)}</span>
                    </span>
                    <span class="font-bold text-slate-350">{globalProgress.toFixed(1)}%</span>
                  </div>

                  <!-- Glowing progress track -->
                  <div class="h-2 w-full rounded-full bg-slate-950 overflow-hidden border border-slate-900/60 p-0.5">
                    <div
                      class="h-full rounded-full bg-gradient-to-r from-purple-500 via-indigo-500 to-emerald-500 transition-all duration-300 shadow-md shadow-indigo-500/20"
                      style="width: {globalProgress}%"
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
            {#if selectedVideoPath}
              <div class="rounded-2xl bg-slate-900/25 border border-slate-900 backdrop-blur-sm p-5 flex flex-col space-y-4 shrink-0 transition-all duration-300">

                <!-- 1. Open File & Folder Buttons (when finished) on Top -->
                {#if selectedItem && selectedItem.status === 'completed' && transcriptionText}
                  <div class="flex space-x-3 w-full pb-1">
                    <button
                      on:click={openTranscriptFile}
                      class="flex-1 py-2.5 rounded-xl bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 text-white font-bold text-xs tracking-wider shadow-lg shadow-emerald-600/20 transition-all active:scale-98 flex items-center justify-center space-x-2 border border-emerald-500/20 cursor-pointer"
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                      </svg>
                      <span>Open File (.txt)</span>
                    </button>
                    <button
                      on:click={openTranscriptFolder}
                      class="flex-1 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-200 font-bold text-xs tracking-wider border border-slate-700 transition-all active:scale-98 flex items-center justify-center space-x-2 cursor-pointer"
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                      </svg>
                      <span>Open Folder</span>
                    </button>
                  </div>
                {/if}

                <!-- 2. Progress Bar and Status Text -->
                <div class="flex flex-col space-y-2">
                  <!-- Progress Bar on Top -->
                  <div class="h-2.5 w-full rounded-full bg-slate-950 overflow-hidden border border-slate-900/60 p-0.5 relative">
                    <div
                      class="h-full rounded-full bg-gradient-to-r from-indigo-500 via-indigo-400 to-purple-500 transition-all duration-300 shadow-md shadow-indigo-500/20"
                      style="width: {progress}%"
                    ></div>
                  </div>

                  <!-- Status Text and Percent below it -->
                  <div class="flex justify-between items-center text-xs">
                    <span class="text-slate-400 font-semibold truncate max-w-[80%]" title={
                      selectedItem ? (
                        selectedItem.status === 'pending' ? 'Waiting in queue...' :
                        selectedItem.status === 'cancelled' ? 'Transcription was cancelled by user.' :
                        selectedItem.status === 'failed' ? `Failed: ${selectedItem.error || 'Unknown error'}` :
                        selectedItem.status === 'completed' ? 'Done! File written adjacent to input.' :
                        currentStage === 1 ? 'Initializing dependencies & caching Whisper model...' :
                        currentStage === 2 ? 'Extracting audio track & running Voice Activity Detection (VAD)...' :
                        currentStage === 3 ? 'Generating subtitles & timecodes...' : 'Starting Python worker runner...'
                      ) : ''
                    }>
                      {#if selectedItem}
                        {#if selectedItem.status === 'pending'}
                          Waiting in queue...
                        {:else if selectedItem.status === 'cancelled'}
                          Transcription was cancelled by user.
                        {:else if selectedItem.status === 'failed'}
                          Failed: {selectedItem.error || 'Unknown error'}
                        {:else if selectedItem.status === 'completed'}
                          Done! File written adjacent to input.
                        {:else if selectedItem.status === 'transcribing'}
                          {#if currentStage === 1}
                            Initializing dependencies & caching Whisper model...
                          {:else if currentStage === 2}
                            Extracting audio track & running Voice Activity Detection (VAD)...
                          {:else if currentStage === 3}
                            Generating subtitles & timecodes...
                          {:else}
                            Starting Python worker runner...
                          {/if}
                        {/if}
                      {/if}
                    </span>
                    <span class="font-bold text-indigo-300 font-mono shrink-0">{progress.toFixed(1)}%</span>
                  </div>
                </div>

                <!-- 3. Details Section (Elapsed & Duration) -->
                <div class="border-t border-b border-slate-900/60 py-3.5 flex flex-col space-y-2.5">
                  <div class="text-[10px] font-bold uppercase tracking-wider text-slate-500 flex items-center space-x-1">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5 text-slate-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    <span>Details</span>
                  </div>
                  <div class="grid grid-cols-2 gap-3.5">
                    <div class="flex items-center justify-between text-xs bg-slate-950/40 border border-slate-900/80 px-3 py-2 rounded-xl">
                      <span class="text-slate-500 font-medium">Elapsed:</span>
                      <span class="font-mono text-indigo-300 font-bold tracking-wider">{formatElapsedTime(elapsedSeconds)}</span>
                    </div>
                    <div class="flex items-center justify-between text-xs bg-slate-950/40 border border-slate-900/80 px-3 py-2 rounded-xl">
                      <span class="text-slate-500 font-medium">Duration:</span>
                      <span class="font-mono text-indigo-300 font-bold tracking-wider">{duration || 'Detecting...'}</span>
                    </div>
                  </div>
                </div>

                <!-- 4. Stepper Stages (Load - preprocessing - transcribe checkmarks) -->
                <div class="flex items-center justify-center text-[10px] sm:text-xs pt-1">
                  <div class="flex items-center justify-center space-x-3 sm:space-x-6 w-full">
                    <!-- Stage 1: Load Model -->
                    <div class="flex items-center space-x-1.5">
                      <div class="h-5 w-5 rounded-full flex items-center justify-center border font-bold text-[10px] transition-all
                        {currentStage > 1 ? 'bg-emerald-950/20 border-emerald-500/30 text-emerald-400' :
                         currentStage === 1 ? 'bg-indigo-950/40 border-indigo-500 text-indigo-400 shadow-md shadow-indigo-500/20 animate-pulse' :
                         'bg-slate-950/40 border-slate-800 text-slate-500'}"
                      >
                        {#if currentStage > 1}
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-emerald-400" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                          </svg>
                        {:else}
                          1
                        {/if}
                      </div>
                      <span class="font-semibold transition-all
                        {currentStage > 1 ? 'text-slate-300 font-medium' :
                         currentStage === 1 ? 'text-indigo-300 font-bold' :
                         'text-slate-500'}"
                      >Load</span>
                    </div>

                    <!-- Arrow separator -->
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-slate-800" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M9 5l7 7-7 7" />
                    </svg>

                    <!-- Stage 2: Audio Preprocess -->
                    <div class="flex items-center space-x-1.5">
                      <div class="h-5 w-5 rounded-full flex items-center justify-center border font-bold text-[10px] transition-all
                        {currentStage > 2 ? 'bg-emerald-950/20 border-emerald-500/30 text-emerald-400' :
                         currentStage === 2 ? 'bg-indigo-950/40 border-indigo-500 text-indigo-400 shadow-md shadow-indigo-500/20 animate-pulse' :
                         'bg-slate-950/40 border-slate-800 text-slate-500'}"
                      >
                        {#if currentStage > 2}
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-emerald-400" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                          </svg>
                        {:else}
                          2
                        {/if}
                      </div>
                      <span class="font-semibold transition-all
                        {currentStage > 2 ? 'text-slate-300 font-medium' :
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
                        {currentStage > 3 ? 'bg-emerald-950/20 border-emerald-500/30 text-emerald-400' :
                         currentStage === 3 ? 'bg-indigo-950/40 border-indigo-500 text-indigo-400 shadow-md shadow-indigo-500/20 animate-pulse' :
                         'bg-slate-950/40 border-slate-800 text-slate-500'}"
                      >
                        {#if currentStage > 3}
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-emerald-400" viewBox="0 0 20 20" fill="currentColor">
                            <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                          </svg>
                        {:else}
                          3
                        {/if}
                      </div>
                      <span class="font-semibold transition-all
                        {currentStage > 3 ? 'text-slate-300 font-medium' :
                         currentStage === 3 ? 'text-indigo-300 font-bold' :
                         'text-slate-500'}"
                      >Transcribe</span>
                    </div>
                  </div>
                </div>

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
                    Transcribed
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
                      <span class="text-[11px] font-semibold font-mono text-indigo-400 bg-indigo-950/30 border border-indigo-900/30 px-2 py-0.5 rounded select-text shrink-0 mt-0.5">
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
                class="flex-1 overflow-y-auto font-mono text-[11px] text-slate-400 mt-2 space-y-1.5 pr-2 custom-scrollbar text-left select-text"
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
      <div class="w-full max-w-2xl bg-slate-900/25 border border-slate-900/60 rounded-3xl p-8 backdrop-blur-md space-y-6 overflow-y-auto max-h-full custom-scrollbar">

        <div class="flex items-center space-x-4">
          <div class="h-12 w-12 rounded-xl bg-indigo-600/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </div>
          <div>
            <h2 class="text-lg font-bold tracking-tight">Configuration & Integration</h2>
            <p class="text-xs text-slate-400">Configure models, timecodes, pre-prompts, and shortcuts</p>
          </div>
        </div>

        <div class="space-y-6">
          <!-- Configuration Panel -->
          <div class="p-5 rounded-2xl bg-slate-950/40 border border-slate-900 flex flex-col space-y-4">
            <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">Configuration</h3>

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

            <!-- Timecode Style Select -->
            <div class="flex items-center justify-between text-xs border-t border-slate-900/60 pt-3">
              <span class="font-medium text-slate-300">Timecode Style:</span>
              <select
                bind:value={selectedTimecodeFormat}
                disabled={transcribing}
                class="bg-slate-950 border border-slate-800 text-indigo-300 font-semibold px-2 py-1.5 rounded focus:outline-none focus:border-indigo-500 disabled:opacity-50 transition-all cursor-pointer text-xs"
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

            <!-- Pre-Prompt Option -->
            <div class="flex flex-col space-y-1.5 border-t border-slate-900/60 pt-3 text-xs">
              <span class="font-medium text-slate-300">Pre-Prompt Text File (Optional):</span>
              {#if prePromptFilePath}
                <div class="flex items-center justify-between p-2 rounded-xl bg-slate-950/60 border border-indigo-500/30 text-xs">
                  <div class="flex items-center space-x-2 min-w-0 flex-1">
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-indigo-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                    </svg>
                    <span class="text-slate-350 truncate font-medium" title={prePromptFilePath}>
                      {getFileName(prePromptFilePath)}
                    </span>
                  </div>
                  <button
                    on:click={clearPrePrompt}
                    disabled={transcribing}
                    class="h-5 w-5 rounded hover:bg-slate-800 text-slate-400 hover:text-red-400 flex items-center justify-center transition-all ml-1 shrink-0 cursor-pointer disabled:opacity-30 disabled:pointer-events-none"
                    title="Remove pre-prompt file"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>
              {:else}
                <div
                  on:click={transcribing ? null : browsePrePrompt}
                  class="p-3 rounded-xl border border-dashed border-slate-800 bg-slate-950/20 hover:bg-slate-950/40 hover:border-indigo-500/40 transition-all flex items-center justify-center space-x-2 text-center
                    {transcribing ? 'opacity-40 cursor-not-allowed pointer-events-none' : 'cursor-pointer group'}"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-slate-550 {transcribing ? '' : 'group-hover:text-indigo-400'} transition-colors shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
                  </svg>
                  <span class="text-[11px] text-slate-500 {transcribing ? '' : 'group-hover:text-slate-350'} transition-colors font-medium">
                    Select or drop pre-prompt .txt file
                  </span>
                </div>
              {/if}
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

          <!-- Dependency Management -->
          <div class="space-y-4 border-t border-slate-900 pt-6">
            <h3 class="text-xs font-bold uppercase tracking-wider text-slate-400">Dependency Management</h3>

            <div class="text-xs text-slate-300 leading-relaxed bg-slate-950/40 p-4 border border-slate-900 rounded-2xl">
              <span class="font-bold text-slate-200 block mb-1">Re-download Dependencies</span>
              If your portable environment dependencies (Python, FFmpeg, faster-whisper, or CUDA libraries) are corrupt, missing, or you wish to reinstall them from scratch, you can trigger the setup wizard again.
            </div>

            <button
              on:click={() => { showEnvPrompt = true; envInstallComplete = false; envInstalling = false; }}
              class="w-full py-3 rounded-xl bg-slate-800 hover:bg-slate-700 font-bold text-xs tracking-wider text-slate-300 hover:text-white border border-slate-700/60 transition-all cursor-pointer"
            >
              Re-run Dependency Setup Wizard
            </button>
          </div>
        </div>
      </div>
    {/if}

  </main>

  <!-- Sleek Mini Footer -->
  <footer class="h-9 border-t border-slate-900 bg-slate-950/20 px-8 flex items-center justify-between text-[10px] text-slate-500 shrink-0">
    <span>v{appVersion}</span>
    <span class="flex items-center space-x-3">
      <span>Model: {selectedModel}</span>
      <span class="h-1 w-1 bg-slate-700 rounded-full"></span>
      <span>By VoidBean</span>
    </span>
  </footer>

  <!-- ENVIRONMENT SETUP OVERLAY MODAL -->
  {#if showEnvPrompt}
    <div class="fixed inset-0 bg-slate-950/85 backdrop-blur-md flex items-center justify-center z-50 p-4 select-none animate-fade-in">
      <div class="w-full max-w-lg bg-slate-900 border border-slate-800/80 rounded-3xl p-6 shadow-2xl flex flex-col space-y-4 text-left relative overflow-hidden">

        {#if envInstalling}
          <!-- Installation Panel -->
          <div class="flex items-center space-x-3">
            <div class="h-9 w-9 rounded-xl bg-indigo-600/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400 shrink-0">
              <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div>
              <h3 class="text-sm font-bold text-indigo-400 animate-pulse">{envInstallStatusText || 'Installing Portable Environment...'}</h3>
              <p class="text-[10px] text-slate-500">Downloading and extracting Python, FFmpeg, and Python libraries. This can take several minutes.</p>
            </div>
          </div>

          <!-- Progress Bar -->
          <div class="space-y-1.5 pt-1">
            <div class="flex justify-between text-[10px] font-bold text-slate-400">
              <span>Overall Progress</span>
              <span class="text-indigo-400 font-mono">{envInstallProgress}%</span>
            </div>
            <div class="w-full h-2 bg-slate-950 rounded-full overflow-hidden border border-slate-900">
              <div
                class="h-full bg-gradient-to-r from-indigo-500 to-purple-500 rounded-full transition-all duration-300"
                style="width: {envInstallProgress}%"
              ></div>
            </div>
          </div>

          <div class="h-44 rounded-xl bg-slate-950 border border-slate-900 p-3.5 flex flex-col min-h-0 shrink-0">
            <div
              bind:this={envConsoleElement}
              class="flex-1 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-1.5 pr-2 custom-scrollbar text-left select-text"
            >
              {#each envInstallLogs as log}
                {#if log.includes("[ERROR]")}
                  <div class="text-red-400 font-bold">{log}</div>
                {:else if log.includes("[WARN]")}
                  <div class="text-amber-400">{log}</div>
                {:else if log.includes("[+]")}
                  <div class="text-emerald-450 font-semibold">{log}</div>
                {:else if log.includes("[*]")}
                  <div class="text-indigo-300">{log}</div>
                {:else}
                  <div>{log}</div>
                {/if}
              {/each}
            </div>
          </div>

          <div class="flex space-x-3 pt-2">
            <button
              on:click={cancelEnvInstallation}
              class="flex-1 py-2.5 rounded-xl bg-red-950/30 hover:bg-red-950/50 text-red-300 hover:text-red-200 font-bold text-xs tracking-wider border border-red-900/40 hover:border-red-800 transition-all active:scale-98 cursor-pointer"
            >
              Cancel Installation
            </button>
          </div>
        {:else if envInstallComplete}
          <!-- Completion Panel -->
          <div class="flex items-start space-x-4">
            {#if envInstallSuccess}
              <div class="h-12 w-12 rounded-2xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-400 shrink-0 mt-0.5 animate-bounce">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              </div>
              <div class="space-y-1 min-w-0 flex-1">
                <h3 class="text-base font-bold text-slate-100">Setup Completed Successfully!</h3>
                <p class="text-xs text-slate-400 leading-relaxed">
                  Portable Python, FFmpeg, and Whisper dependencies have been downloaded and configured. The transcription studio is ready to use!
                </p>
              </div>
            {:else}
              <div class="h-12 w-12 rounded-2xl bg-red-500/10 border border-red-500/20 flex items-center justify-center text-red-400 shrink-0 mt-0.5">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
              </div>
              <div class="space-y-1 min-w-0 flex-1">
                <h3 class="text-base font-bold text-slate-100">Installation Incomplete</h3>
                <p class="text-xs text-slate-400 leading-relaxed">
                  The setup was cancelled or encountered an error. You can click 'Retry' to resume, or 'Close' to exit.
                 </p>
              </div>
            {/if}
          </div>

          <!-- Mini Log View in completed screen -->
          <div class="h-44 rounded-xl bg-slate-950 border border-slate-900/60 p-3.5 flex flex-col min-h-0 shrink-0">
            <div class="text-[9px] font-bold text-slate-500 uppercase tracking-wider pb-1.5 border-b border-slate-900 mb-1.5 flex justify-between items-center shrink-0">
              <span>Setup Logs</span>
              <span class="text-slate-600 font-mono text-[8px]">{envInstallLogs.length} lines</span>
            </div>
            <div
              bind:this={envConsoleElement}
              class="flex-1 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-1.5 pr-2 custom-scrollbar text-left select-text"
            >
              {#each envInstallLogs as log}
                {#if log.includes("[ERROR]")}
                  <div class="text-red-400 font-bold">{log}</div>
                {:else if log.includes("[WARN]")}
                  <div class="text-amber-400">{log}</div>
                {:else if log.includes("[+]")}
                  <div class="text-emerald-450 font-semibold">{log}</div>
                {:else if log.includes("[*]")}
                  <div class="text-indigo-300">{log}</div>
                {:else}
                  <div>{log}</div>
                {/if}
              {/each}
            </div>
          </div>

          <div class="flex space-x-3 pt-2">
            {#if !envInstallSuccess}
              <button
                on:click={startEnvInstallation}
                class="flex-1 py-2.5 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white font-bold text-xs tracking-wider transition-all active:scale-98 shadow-lg shadow-indigo-600/10 cursor-pointer animate-pulse"
              >
                Retry
              </button>
            {/if}
            <button
              on:click={() => showEnvPrompt = false}
              class="px-5 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-300 font-semibold text-xs tracking-wider border border-slate-700/60 transition-all cursor-pointer {envInstallSuccess ? 'flex-1' : ''}"
            >
              Close
            </button>
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
              <h3 class="text-base font-bold text-slate-100">Setup Portable Environment</h3>
              <p class="text-xs text-slate-400 leading-relaxed">
                VoidTranscribe runs offline using a self-contained portable Python environment and FFmpeg binaries to keep your system clean and avoid configuration issues.
              </p>
            </div>
          </div>

          <div class="text-[11px] text-slate-350 leading-relaxed bg-slate-950/45 p-4 rounded-2xl border border-slate-900/60">
            <span class="font-bold block text-slate-350 mb-1">What this does:</span>
            - Downloads and sets up an isolated Python runtime (3.10.11)<br/>
            - Configures local pip and installs <code class="text-indigo-400 font-semibold font-mono">faster-whisper</code><br/>
            - Installs local CUDA DLLs for GPU acceleration (if supported)<br/>
            - Downloads and extracts static FFmpeg binaries inside the cache folder<br/>
            - Takes about 2.5 GB of disk space
          </div>

          <div class="flex space-x-3 pt-2">
            <button
              on:click={startEnvInstallation}
              class="flex-1 py-2.5 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white font-bold text-xs tracking-wider transition-all active:scale-98 shadow-lg shadow-indigo-600/10 cursor-pointer"
            >
              Start Automated Install
            </button>
            <button
              on:click={() => showEnvPrompt = false}
              class="px-4 py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-300 font-semibold text-xs tracking-wider border border-slate-700/60 transition-all cursor-pointer"
            >
              Cancel
            </button>
          </div>
        {/if}

      </div>
    </div>
  {/if}

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
              class="flex-1 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-1.5 pr-2 custom-scrollbar text-left select-text"
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
              class="flex-1 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-1.5 pr-2 custom-scrollbar text-left select-text"
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
            on:click={() => { selectedDeviceMode = 'cpu'; showVramWarning = false; }}
            class="w-full py-2.5 rounded-xl bg-slate-800 hover:bg-slate-700 text-slate-200 font-bold text-xs tracking-wider transition-all active:scale-98 border border-slate-700/40 cursor-pointer"
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
