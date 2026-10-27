package qui

// The Debug domain: qui's own engine diagnostics — layout warnings now, and
// the runtime switches for the QUI_DEBUG_* checks (see protocol_flags.go).

type debugDiagnosticsResult struct {
	Diagnostics []LayoutDiagnostic `json:"diagnostics"`
}

func debugDomain() ProtocolDomain {
	return ProtocolDomain{
		Name:        "Debug",
		Description: "qui engine diagnostics and runtime debug switches.",
		Commands: append([]ProtocolCommand{
			NewProtocolCommand("getDiagnostics", "Layout warnings and errors: overflow, out-of-bounds, flex misconfiguration. Read when a node you expect is missing or misplaced.",
				func(ctx *ProtocolContext, _ empty) (debugDiagnosticsResult, error) {
					d := ctx.Window.DebugLayoutDiagnosticsSynced()
					if d == nil {
						d = []LayoutDiagnostic{}
					}
					return debugDiagnosticsResult{Diagnostics: d}, nil
				}),
		}, debugFlagCommands()...),
	}
}
