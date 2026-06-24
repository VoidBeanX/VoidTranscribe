import sys
import os
import traceback
import argparse
import logging

# Force stdout and stderr to use UTF-8 encoding to prevent UnicodeEncodeErrors on Windows consoles (e.g., CP1252 / charmap codec errors)
if hasattr(sys.stdout, 'reconfigure'):
    sys.stdout.reconfigure(encoding='utf-8')
if hasattr(sys.stderr, 'reconfigure'):
    sys.stderr.reconfigure(encoding='utf-8')

# Set up standard logging to print to stdout so Wails can stream it
logging.basicConfig(
    level=logging.INFO,
    format='[LOG] %(name)s: %(message)s',
    stream=sys.stdout
)

# Add local site-packages/nvidia folders to DLL search path on Windows
if os.name == 'nt':
    try:
        engine_dir = os.path.dirname(os.path.abspath(__file__))
        site_packages = os.path.join(engine_dir, 'Lib', 'site-packages')
        print(f"[LOG] DLL config: __file__={__file__}")
        print(f"[LOG] DLL config: engine_dir={engine_dir}")
        print(f"[LOG] DLL config: site_packages={site_packages} (exists: {os.path.exists(site_packages)})")
        if os.path.exists(site_packages):
            nvidia_dir = os.path.join(site_packages, 'nvidia')
            print(f"[LOG] DLL config: nvidia_dir={nvidia_dir} (exists: {os.path.exists(nvidia_dir)})")
            if os.path.exists(nvidia_dir):
                for root, dirs, files in os.walk(nvidia_dir):
                    if root.endswith('bin') or root.endswith('lib'):
                        try:
                            print(f"[LOG] DLL config: adding DLL directory: {root}")
                            os.add_dll_directory(root)
                        except Exception as e:
                            print(f"[LOG] DLL config: failed to add DLL directory {root}: {str(e)}")
    except Exception as e:
        print(f"[LOG] Warning: failed to configure local DLL directory scan: {str(e)}")

def seconds_to_timecode(seconds, fps=29.97):
    """Converts seconds to HH:MM:SS:FF timecode style."""
    total_frames = int(round(seconds * fps))
    frames_per_sec = int(round(fps))
    if frames_per_sec == 0:
        frames_per_sec = 30

    hrs = total_frames // (3600 * frames_per_sec)
    remaining = total_frames % (3600 * frames_per_sec)
    mins = remaining // (60 * frames_per_sec)
    remaining = remaining % (60 * frames_per_sec)
    secs = remaining // frames_per_sec
    frames = remaining % frames_per_sec

    return f"{hrs:02d}:{mins:02d}:{secs:02d}:{frames:02d}"

def seconds_to_ms_timecode(seconds, separator=","):
    """Converts seconds to HH:MM:SS,mmm or HH:MM:SS.mmm style."""
    hrs = int(seconds // 3600)
    mins = int((seconds % 3600) // 60)
    secs = int(seconds % 60)
    ms = int(round((seconds - int(seconds)) * 1000))
    if ms >= 1000:
        ms = 999
    return f"{hrs:02d}:{mins:02d}:{secs:02d}{separator}{ms:03d}"

def run_transcription_on_model(model, video_path, output_path, format_style):
    print("[LOG] Stage 2/3: Audio feature extraction & VAD preprocessing...")
    segments, info = model.transcribe(video_path, beam_size=5)
    print("[LOG] Stage 2/3: Audio feature extraction & VAD preprocessing... Done!")

    total_duration = info.duration
    print(f"[LOG] Total audio duration: {total_duration:.2f} seconds")
    print(f"[LOG] Language detected: '{info.language}' (Probability: {info.language_probability:.2f})")
    print("[LOG] Stage 3/3: Streaming transcript segments to file...")

    # Detect video framerate for NLE timecode formats
    fps = 29.97
    if format_style in ["davinci", "premiere", "avid", "fcp"]:
        try:
            import av
            container = av.open(video_path)
            video_stream = next((s for s in container.streams if s.type == 'video'), None)
            if video_stream:
                if video_stream.average_rate:
                    fps = float(video_stream.average_rate)
                elif video_stream.base_rate:
                    fps = float(video_stream.base_rate)
                if fps <= 0 or fps > 200:
                    fps = 29.97
            print(f"[LOG] Video framerate detected: {fps:.3f} fps")
        except Exception as e:
            print(f"[LOG] Warning: Framerate detection failed ({str(e)}). Defaulting to 29.97 fps.")

    # Open output file with UTF-8 encoding
    with open(output_path, "w", encoding="utf-8") as out_file:
        for segment in segments:
            # Format timecode based on selected style
            if format_style == "davinci" or format_style == "premiere" or format_style == "avid":
                # NLE Marker/Locator timecode: [HH:MM:SS:FF] (Start only)
                tc = seconds_to_timecode(segment.start, fps)
                timecode = f"[{tc}]"
            elif format_style == "fcp":
                # FCP Range timecode: [HH:MM:SS:FF -> HH:MM:SS:FF]
                tc_start = seconds_to_timecode(segment.start, fps)
                tc_end = seconds_to_timecode(segment.end, fps)
                timecode = f"[{tc_start} -> {tc_end}]"
            elif format_style == "srt":
                # SubRip timecode: 00:01:20,140 --> 00:01:20,380
                tc_start = seconds_to_ms_timecode(segment.start, ",")
                tc_end = seconds_to_ms_timecode(segment.end, ",")
                timecode = f"{tc_start} --> {tc_end}"
            elif format_style == "vtt":
                # WebVTT timecode: 00:01:20.140 --> 00:01:20.380
                tc_start = seconds_to_ms_timecode(segment.start, ".")
                tc_end = seconds_to_ms_timecode(segment.end, ".")
                timecode = f"{tc_start} --> {tc_end}"
            else:
                # Default Seconds range: [109.98 -> 110.38]
                timecode = f"[{segment.start:.2f} -> {segment.end:.2f}]"

            line = f"{timecode} {segment.text.strip()}\n"

            # Write to the file
            out_file.write(line)
            out_file.flush()

            # Print to stdout for Wails Go app to stream in real-time
            print(f"[SEGMENT] {timecode} {segment.text.strip()}")

            # Calculate progress percentage
            if total_duration > 0:
                progress_pct = min(100.0, (segment.end / total_duration) * 100.0)
                print(f"[PROGRESS] {progress_pct:.1f}")

    print("[PROGRESS] 100.0")
    print("[LOG] Stage 3/3: Streaming transcript segments to file... Complete!")

def load_and_transcribe(device_mode, video_path, output_path, format_style, model_size):
    from faster_whisper import WhisperModel

    def create_whisper_model(device, compute_type):
        model = WhisperModel(model_size, device=device, compute_type=compute_type, local_files_only=True)
        if "v3" in model_size.lower():
            model.feature_extractor.mel_filters = model.feature_extractor.get_mel_filters(
                model.feature_extractor.sampling_rate, model.feature_extractor.n_fft, n_mels=128
            )
        except Exception as e:
            print(f"[LOG] Model '{model_size}' not found in local cache (or offline check failed: {str(e)}). Querying Hugging Face Hub...")
            model = WhisperModel(model_size, device=device, compute_type=compute_type, local_files_only=False)
            if "v3" in model_size.lower():
                model.feature_extractor.mel_filters = model.feature_extractor.get_mel_filters(
                    model.feature_extractor.sampling_rate, model.feature_extractor.n_fft, n_mels=128
                )
        return model

    if device_mode == "cuda":
        print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' strictly on GPU (CUDA, float16)...")
        try:
            model = create_whisper_model(device="cuda", compute_type="float16")
            print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' strictly on GPU (CUDA, float16)... Found!")
            run_transcription_on_model(model, video_path, output_path, format_style)
            print("[LOG] Transcription completed successfully on GPU!")
        except Exception as e:
            print(f"[LOG] [CRITICAL ERROR] GPU execution failed: {str(e)}")
            print("[LOG] Fallback to CPU is disabled under current 'GPU Only' settings.")
            traceback.print_exc()
            sys.exit(1)

    elif device_mode == "cpu":
        print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' strictly on CPU (int8)...")
        try:
            model = create_whisper_model(device="cpu", compute_type="int8")
            print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' strictly on CPU (int8)... Found!")
            run_transcription_on_model(model, video_path, output_path, format_style)
            print("[LOG] Transcription completed successfully on CPU!")
        except Exception as e:
            print(f"[LOG] [CRITICAL ERROR] CPU execution failed: {str(e)}")
            traceback.print_exc()
            sys.exit(1)

    elif device_mode == "auto":
        print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' in Auto mode (trying GPU first)...")

        # Try GPU
        gpu_success = False
        try:
            model = create_whisper_model(device="cuda", compute_type="float16")
            print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' in Auto mode ... Found (GPU)!")
            run_transcription_on_model(model, video_path, output_path, format_style)
            print("[LOG] Transcription completed successfully on GPU!")
            gpu_success = True
        except Exception as e:
            print(f"[LOG] GPU execution or model load failed: {str(e)}")
            print("[LOG] 'Auto' mode active: falling back to CPU execution (int8)...")

        # Fallback to CPU
        if not gpu_success:
            try:
                model = create_whisper_model(device="cpu", compute_type="int8")
                print(f"[LOG] Stage 1/3: Loading Whisper model '{model_size}' in Auto mode ... Found (CPU fallback)!")
                run_transcription_on_model(model, video_path, output_path, format_style)
                print("[LOG] Transcription completed successfully on CPU!")
            except Exception as e:
                print(f"[LOG] [CRITICAL ERROR] Fallback CPU execution failed: {str(e)}")
                traceback.print_exc()
                sys.exit(1)

def main():
    parser = argparse.ArgumentParser(description="VoidTranscribe Portable Python Inference Worker")
    parser.add_argument("video_path", help="Path to the input video file")
    parser.add_argument("--device", choices=["cuda", "cpu", "auto"], default="cuda",
                        help="Inference execution device. cuda = GPU Only (Default), cpu = CPU Only, auto = GPU with CPU fallback.")
    parser.add_argument("--format", choices=["davinci", "premiere", "avid", "fcp", "seconds", "srt", "vtt"], default="davinci",
                        help="Timecode format style. Default is davinci.")
    parser.add_argument("--model", choices=["distil-small.en", "distil-medium.en", "distil-large-v2", "distil-large-v3"], default="distil-medium.en",
                        help="Whisper model size. Default is distil-medium.en.")

    args = parser.parse_args()

    video_path = os.path.abspath(args.video_path)
    if not os.path.exists(video_path):
        print(f"[LOG] Error: File '{video_path}' does not exist.")
        sys.exit(1)

    output_path = video_path + ".txt"
    print(f"[LOG] Initializing transcription pipeline.")
    print(f"[LOG] Selected Execution Mode: {args.device.upper()}")
    print(f"[LOG] Selected Whisper Model: {args.model.upper()}")
    print(f"[LOG] Selected Timecode Format: {args.format.upper()}")
    print(f"[LOG] Input video: {video_path}")
    print(f"[LOG] Output path: {output_path}")

    # Verify faster_whisper import
    try:
        import faster_whisper
    except ImportError as e:
        print(f"[LOG] ImportError: Failed to import faster_whisper: {str(e)}")
        print("[LOG] Ensure dependencies are provisioned in engine/Lib/site-packages.")
        sys.exit(1)

    load_and_transcribe(args.device, video_path, output_path, args.format, args.model)
    # Flush output streams and force terminate process to prevent CUDA DLL unloading hangs on Windows
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(0)

if __name__ == "__main__":
    main()
