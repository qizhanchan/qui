package diagram

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeCompressedDrawioPayload(t *testing.T) {
	model := `<mxGraphModel pageWidth="500" pageHeight="300"><root>` +
		`<mxCell id="0"/><mxCell id="1" parent="0"/>` +
		`<mxCell id="a" value="A" style="rounded=1;whiteSpace=wrap;html=1;" vertex="1" parent="1"><mxGeometry x="40" y="40" width="120" height="50" as="geometry"/></mxCell>` +
		`<mxCell id="b" value="B" style="ellipse;whiteSpace=wrap;html=1;" vertex="1" parent="1"><mxGeometry x="320" y="40" width="120" height="50" as="geometry"/></mxCell>` +
		`<mxCell id="e" value="link" style="edgeStyle=orthogonalEdgeStyle;rounded=1;endArrow=block;" edge="1" parent="1" source="a" target="b"><mxGeometry relative="1" as="geometry"/></mxCell>` +
		`</root></mxGraphModel>`
	var compressed bytes.Buffer
	zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(model)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(compressed.Bytes())
	inputXML := `<mxfile><diagram id="compressed" name="Compressed">` + payload + `</diagram></mxfile>`
	doc, err := DecodeDrawio([]byte(inputXML))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Cell("e"); got == nil || got.Label != "link" || got.Style.Route != RouteRoundedOrthogonal {
		t.Fatalf("edge not decoded: %+v", got)
	}
}

func TestDrawioFloatingEndpoints(t *testing.T) {
	inputXML := `<mxGraphModel pageWidth="400" pageHeight="240"><root>` +
		`<mxCell id="0"/><mxCell id="1" parent="0"/>` +
		`<mxCell id="a" value="A" style="rounded=0;" vertex="1" parent="1"><mxGeometry x="30" y="30" width="100" height="50" as="geometry"/></mxCell>` +
		`<mxCell id="e" style="endArrow=open;" edge="1" parent="1" source="a"><mxGeometry relative="1" as="geometry"><mxPoint x="300" y="180" as="targetPoint"/></mxGeometry></mxCell>` +
		`</root></mxGraphModel>`
	doc, err := DecodeDrawio([]byte(inputXML))
	if err != nil {
		t.Fatal(err)
	}
	e := doc.Cell("e")
	if e == nil || e.Geometry.TargetPoint == nil || e.Geometry.TargetPoint.X != 300 || e.Geometry.TargetPoint.Y != 180 {
		t.Fatalf("floating endpoint not decoded: %+v", e)
	}
}

func TestDrawioLimits(t *testing.T) {
	inputXML := `<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel>`
	_, err := DecodeDrawioWithLimits([]byte(inputXML), DrawioLimits{MaxBytes: 8, MaxCells: 10, MaxTextBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}
