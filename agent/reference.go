package agent

import (
	"fmt"
	"strings"

	"github.com/qizhanchan/qui"
)

// protocolReference renders the method reference half of /llm.txt from the
// live schema, so the documented parameters are exactly the decoded ones.
func protocolReference() []byte {
	var b strings.Builder
	b.WriteString("\n==================================================================\n")
	b.WriteString("METHOD REFERENCE (generated from the live schema; GET /json/protocol for JSON)\n")
	b.WriteString("==================================================================\n")
	schema := qui.ProtocolSchema()
	domains, _ := schema["domains"].([]any)
	// Agent first: it is where an agent starts.
	order := []map[string]any{}
	for _, d := range domains {
		if dm := d.(map[string]any); dm["domain"] == "Agent" {
			order = append([]map[string]any{dm}, order...)
		} else {
			order = append(order, dm)
		}
	}
	for _, dm := range order {
		name := dm["domain"].(string)
		fmt.Fprintf(&b, "\n## %s\n", name)
		if d, ok := dm["description"].(string); ok {
			writeWrapped(&b, d, "   ")
		}
		cmds, _ := dm["commands"].([]any)
		for _, c := range cmds {
			cm := c.(map[string]any)
			fmt.Fprintf(&b, "\n%s.%s\n", name, cm["name"])
			if d, ok := cm["description"].(string); ok {
				writeWrapped(&b, d, "   ")
			}
			writeParams(&b, "params", cm["parameters"])
			writeParams(&b, "returns", cm["returns"])
		}
		evs, _ := dm["events"].([]any)
		for _, e := range evs {
			em := e.(map[string]any)
			fmt.Fprintf(&b, "\nevent %s.%s\n", name, em["name"])
			if d, ok := em["description"].(string); ok {
				writeWrapped(&b, d, "   ")
			}
		}
	}
	return []byte(b.String())
}

func writeParams(b *strings.Builder, label string, v any) {
	list, _ := v.([]any)
	if len(list) == 0 {
		return
	}
	fmt.Fprintf(b, "   %s:\n", label)
	for _, p := range list {
		pm := p.(map[string]any)
		typ, _ := pm["type"].(string)
		if items, ok := pm["items"].(map[string]any); ok {
			typ = "[]" + fmt.Sprint(items["type"])
		}
		opt := ""
		if o, _ := pm["optional"].(bool); o {
			opt = "?"
		}
		line := fmt.Sprintf("     %s%s: %s", pm["name"], opt, typ)
		if d, ok := pm["description"].(string); ok {
			line += " — " + d
		}
		b.WriteString(line + "\n")
	}
}

// writeWrapped writes text wrapped at ~72 columns with an indent.
func writeWrapped(b *strings.Builder, text, indent string) {
	line := indent
	for _, word := range strings.Fields(text) {
		if len(line)+len(word)+1 > 72 && strings.TrimSpace(line) != "" {
			b.WriteString(strings.TrimRight(line, " ") + "\n")
			line = indent
		}
		line += word + " "
	}
	if strings.TrimSpace(line) != "" {
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
}
