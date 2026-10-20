package diagram

import (
	"encoding/json"
	"errors"
	"io"
)

// EncodeJSON serializes a document using the native diagram schema.
func EncodeJSON(doc *Document) []byte {
	if doc == nil {
		return nil
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil
	}
	return data
}

// DecodeJSONBytes parses and validates a native diagram document.
func DecodeJSONBytes(data []byte) (*Document, error) {
	if len(data) == 0 {
		return nil, errors.New("diagram: empty JSON document")
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	normalizeDocument(&doc)
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DecodeJSON reads and validates a native diagram document.
func DecodeJSON(r io.Reader) (*Document, error) {
	const maxDocument = 64 << 20
	data, err := io.ReadAll(io.LimitReader(r, maxDocument+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDocument {
		return nil, errors.New("diagram: JSON document exceeds 64 MiB")
	}
	return DecodeJSONBytes(data)
}

// WriteJSON writes the native diagram schema.
func WriteJSON(w io.Writer, doc *Document) error {
	data := EncodeJSON(doc)
	_, err := w.Write(data)
	return err
}

func normalizeDocument(doc *Document) {
	if doc == nil || len(doc.Pages) == 0 {
		return
	}
	if doc.ActivePage < 0 || doc.ActivePage >= len(doc.Pages) {
		doc.ActivePage = 0
	}
	for _, p := range doc.Pages {
		if p == nil {
			continue
		}
		if p.Width <= 0 {
			p.Width = 800
		}
		if p.Height <= 0 {
			p.Height = 600
		}
		if p.Background == "" {
			p.Background = "#ffffff"
		}
		if len(p.Layers) == 0 {
			p.Layers = []Layer{{ID: "layer-1", Name: "Layer-1", Default: true}}
		}
		defaultLayer := ""
		for i := range p.Layers {
			if p.Layers[i].ID == "" {
				p.Layers[i].ID = "layer-" + string(rune('1'+i))
			}
			if p.Layers[i].Default || defaultLayer == "" {
				defaultLayer = p.Layers[i].ID
			}
		}
		if len(p.Cells) == 0 {
			continue
		}
		for _, c := range p.Cells {
			if c == nil {
				continue
			}
			if c.Type == "" {
				if c.Source.Cell != "" || c.Target.Cell != "" {
					c.Type = CellEdge
				} else {
					c.Type = CellVertex
				}
			}
			if c.Layer == "" {
				c.Layer = defaultLayer
			}
			if c.Style.Shape == "" && c.Type != CellEdge && c.Type != CellGroup {
				c.Style.Shape = ShapeRect
			}
			if c.Style.Route == "" && c.Type == CellEdge {
				c.Style.Route = RouteOrthogonal
			}
		}
	}
}
