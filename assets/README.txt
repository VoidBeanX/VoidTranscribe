VoidTranscribe is designed to be a standalone executable that builds it's configuration after first run.

Space Used: 2.5GB (small model) to 5GB (large model)
- VoidTranscribe.exe is about 10MB
- cache subfolder with everything downloaded is ~2.5GB
- %USERPROFILE%\.cache\huggingface\hub with distil-large-v3 is ~2.8GB

It will create:
- A config.json file to keep track of settings and preferences
- Add README.txt, this file, to explain what the program is and how to use it
- Create a cache subfolder. This is downloaded assets such as:
    - the Whisper python scripts, under cache/engine
    - ffmpeg for audio processing
- Hugging face will download their models to the cache folder, under %USERPROFILE%\.cache\huggingface\hub


To fully uninstall, you can delete the path VoidTranscribe.exe is at and all subfolders, then %USERPROFILE%\.cache\huggingface\hub
