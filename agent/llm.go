package agent

import _ "embed"

// guideTxt is the hand-written half of /llm.txt: concepts, selector
// rules, recipes and pitfalls. The method reference half is generated from
// the live protocol schema (reference.go), so it cannot drift from the code.
//
//go:embed llm.txt
var guideTxt []byte

func llmText() []byte {
	return append(append([]byte(nil), guideTxt...), protocolReference()...)
}
