package agent

import _ "embed"

// llmTxt is the model-facing self-doc for the agent surface. Served
// at GET /llm.txt. The .txt source lives in this directory so it
// can be edited without rebuilding code.
//
//go:embed llm.txt
var llmTxt []byte
