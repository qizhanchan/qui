package qui

// Root's protocol domains. Subpackages register theirs from their own init
// (htmlcss: CSS and the DOM projector; reactive: Reactive), and the agent
// package adds the transport-level ones (Target, Browser, Schema).
func init() {
	RegisterProtocolDomain(accessibilityDomain())
	RegisterProtocolDomain(animationDomain())
	RegisterProtocolDomain(emulationDomain())
	RegisterProtocolDomain(agentDomain())
	RegisterProtocolDomain(debugDomain())
	RegisterProtocolDomain(domDomain())
	RegisterProtocolDomain(inputDomain())
	RegisterProtocolDomain(logDomain())
	RegisterProtocolDomain(overlayDomain())
	RegisterProtocolDomain(pageDomain())
	RegisterProtocolDomain(runtimeDomain())
}
