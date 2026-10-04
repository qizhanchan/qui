// Package agent exposes a qui Window to external AI agents via a
// local HTTP + Server-Sent-Events server. It is the "wire" half of
// qui's ai-native operability story; the in-process Go API lives on
// *qui.Window (Find / Click / Type / SnapshotAnnotated / etc.) and
// is always available. This package adds:
//
//   - GET  /llm.txt        — self-documenting reference (embed)
//   - GET  /tree           — accessibility tree (JSON); optional
//     selector / maxDepth / leaves filters
//   - GET  /screenshot     — PNG (scale / annotate / region)
//   - GET  /diagnostics    — layout warnings
//   - GET  /wait           — block until idle / overlay appears /
//     selector appears|gone / tree stable
//   - GET  /events         — SSE stream of dispatched events
//   - GET  /console        — recent recorded events
//   - POST /act            — perform an action atomically (selector-
//     targeted, or click by raw x/y)
//   - GET  /health         — liveness/readiness probe (JSON)
//
// The server defaults to a Unix domain socket at
// $TMPDIR/qui-agent-<pid>.sock (no port collisions, not visible in
// netstat). TCP is opt-in via BindTCP / QUI_AGENT_TCP env, and
// requires QUI_AGENT_TOKEN bearer auth when bound.
//
// Threading: every endpoint that mutates window state (POST /act,
// GET /wait) reaches the main goroutine via Window.PostJob. Read-
// only endpoints (/tree, /screenshot, /diagnostics) sample
// snapshot-style state directly under their own internal locking;
// the per-frame draw races are absorbed by the immutable nature of
// the snapshot returned (image / tree are deep copies).
//
// Environment variables (handled by BindEnv):
//
//	QUI_AGENT=1            — start a UDS listener
//	QUI_AGENT_SOCK=/p.sock — pin the UDS path (implies QUI_AGENT=1). Without
//	                         it the path carries the PID, and a client that
//	                         auto-discovers the newest socket can attach to a
//	                         different app when several are running.
//	QUI_AGENT_TCP=:9119    — also start a TCP listener on the given addr
//	QUI_AGENT_TOKEN=xyz    — require Authorization: Bearer xyz on TCP
//
// The package is pure Go and imports only the standard library +
// root qui. No widgets / media / scene3d / webview dependencies.
package agent
