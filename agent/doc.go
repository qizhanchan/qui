// Package agent serves qui's inspection protocol: the transport for AI
// agents, tools and Chrome DevTools to observe and drive a running app.
//
// The protocol is CDP-shaped (Chrome DevTools Protocol): domains of typed
// commands and events, JSON-RPC messages {id, method, params, sessionId}.
// The domains themselves live in root's registry (qui.RegisterProtocolDomain
// — Agent, DOM, Accessibility, Input, Page, Overlay, Log, Runtime,
// Emulation, Animation, Debug), with htmlcss adding CSS and its DOM
// projection and reactive adding Reactive. This package knows none of them
// by name: it adds transport, sessions, targets and the event buffer.
//
//	POST /rpc                      one call: {method, params, target?}
//	GET  /devtools/browser/<id>    WebSocket: app-level session
//	GET  /devtools/page/<target>   WebSocket: session bound to one window
//	GET  /json/version, /json/list, /json/protocol   CDP discovery + schema
//	GET  /llm.txt                  the guide for agents + method reference
//
// One server covers every window of the process (qui.LiveWindows); windows
// are targets ("window-1", …). The server listens on a Unix domain socket
// by default ($TMPDIR/qui-agent-<pid>.sock, owner-only) and writes a
// discovery file ($TMPDIR/qui-agent/<app>.json) so clients find the app by
// name. TCP is opt-in and requires a bearer token.
//
// Environment variables (handled by BindEnv):
//
//	QUI_AGENT=1            — start a UDS listener
//	QUI_AGENT_SOCK=/p.sock — pin the UDS path (implies QUI_AGENT=1)
//	QUI_AGENT_TCP=:9119    — also start a TCP listener on the given addr
//	QUI_AGENT_TOKEN=xyz    — require Authorization: Bearer xyz on TCP
//	QUI_AGENT_DEVTOOLS=1   — let Chrome DevTools attach over TCP (the token
//	                         then also travels as /devtools/t/<token>/…)
//
// agent/client is a stdlib-only client for the protocol; cmd/qui-agent is a
// CLI and MCP server built on it.
package agent
