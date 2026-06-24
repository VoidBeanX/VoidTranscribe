package assets

import _ "embed"

//go:embed setup_env.ps1
var SetupScript []byte

//go:embed README.txt
var Readme []byte

//go:embed transcribe.py
var Transcribe []byte