export namespace main {
	
	export class AppConfig {
	    timecodeFormat: string;
	    selectedModel: string;
	    selectedDeviceMode: string;
	    prePromptFilePath: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timecodeFormat = source["timecodeFormat"];
	        this.selectedModel = source["selectedModel"];
	        this.selectedDeviceMode = source["selectedDeviceMode"];
	        this.prePromptFilePath = source["prePromptFilePath"];
	    }
	}
	export class RequirementsStatus {
	    pythonExists: boolean;
	    transcribeScriptOk: boolean;
	    ffmpegExists: boolean;
	    fasterWhisperReady: boolean;
	    cudaLibsExists: boolean;
	    isRegistered: boolean;
	    modelDirSize: string;
	
	    static createFrom(source: any = {}) {
	        return new RequirementsStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pythonExists = source["pythonExists"];
	        this.transcribeScriptOk = source["transcribeScriptOk"];
	        this.ffmpegExists = source["ffmpegExists"];
	        this.fasterWhisperReady = source["fasterWhisperReady"];
	        this.cudaLibsExists = source["cudaLibsExists"];
	        this.isRegistered = source["isRegistered"];
	        this.modelDirSize = source["modelDirSize"];
	    }
	}
	export class VideoValidationResult {
	    isValid: boolean;
	    hasAudio: boolean;
	    errorMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new VideoValidationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isValid = source["isValid"];
	        this.hasAudio = source["hasAudio"];
	        this.errorMessage = source["errorMessage"];
	    }
	}

}

