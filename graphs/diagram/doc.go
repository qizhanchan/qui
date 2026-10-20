// Package diagram implements qui's native draw.io-class diagram model,
// renderer and editor.
//
// The package is deliberately split into three layers:
//
//   - Document is a serializable cell graph (pages, layers, vertices,
//     edges, groups and geometry). It contains no widget or paint state.
//   - RenderImage / WritePNG / WriteSVG / WritePDF paint a document
//     headlessly through qui.Canvas, without a Window.
//   - Diagram is a retained qui widget that hosts the document, handles
//     viewport navigation, selection, editing, undo/redo and accessibility.
//
// draw.io XML is supported as an interoperability and parity format. The
// native model remains the source of truth; no JavaScript runtime or
// embedded browser is used at runtime.
package diagram
