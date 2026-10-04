package qui

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"hash/fnv"
	"image"
)

// Raster images in the PDF backend.
//
// A picture becomes an image XObject: the pixels are written once as a
// Flate-compressed /DeviceRGB stream, and each placement is a `cm` + `Do`
// in the page's content stream. Transparency rides on an /SMask — a second,
// greyscale XObject holding the alpha channel — because PDF has no
// per-pixel alpha in the colour stream itself.
//
// Sharing matters here: a logo repeated on every page of a long document,
// or a watermark on all forty pages, is ONE stream referenced forty times.
// Identity is the pixel bytes rather than the image.Image value, because an
// interface holding a non-comparable dynamic type panics as a map key, and
// two decodes of the same file are two different values anyway.

// pdfImage is one embedded picture, already encoded.
type pdfImage struct {
	name   string // resource name inside a page's /XObject dict
	w, h   int
	rgb    []byte // zlib-compressed RGB triples, row-major from the top
	alpha  []byte // zlib-compressed 8-bit alpha; nil when fully opaque
	id     int    // object id, assigned at write time
	maskID int
}

// addImage encodes img (or returns the existing entry for the same pixels)
// and returns it.
func (d *PDFDoc) addImage(img image.Image) *pdfImage {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	rgb, alpha := imagePDFSamples(img)
	key := imageSampleKey(b.Dx(), b.Dy(), rgb, alpha)
	if existing, ok := d.imageIndex[key]; ok {
		return existing
	}
	entry := &pdfImage{
		name: fmt.Sprintf("Im%d", len(d.images)+1),
		w:    b.Dx(),
		h:    b.Dy(),
		rgb:  pdfDeflate(rgb),
	}
	if alpha != nil {
		entry.alpha = pdfDeflate(alpha)
	}
	if d.imageIndex == nil {
		d.imageIndex = map[uint64]*pdfImage{}
	}
	d.imageIndex[key] = entry
	d.images = append(d.images, entry)
	return entry
}

// imagePDFSamples flattens the image into 8-bit RGB plus, when anything is
// translucent, a matching alpha plane. Go's colour model is
// alpha-premultiplied; PDF's is not, so the colour channels are divided
// back out — otherwise a semi-transparent pixel darkens toward black
// exactly the way it does when the two conventions are confused.
func imagePDFSamples(img image.Image) (rgb, alpha []byte) {
	b := img.Bounds()
	n := b.Dx() * b.Dy()
	rgb = make([]byte, 0, n*3)
	alphaPlane := make([]byte, 0, n)
	translucent := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a == 0 {
				rgb = append(rgb, 0, 0, 0)
				alphaPlane = append(alphaPlane, 0)
				translucent = true
				continue
			}
			if a < 0xffff {
				translucent = true
				r = r * 0xffff / a
				g = g * 0xffff / a
				bl = bl * 0xffff / a
			}
			rgb = append(rgb, byte(r>>8), byte(g>>8), byte(bl>>8))
			alphaPlane = append(alphaPlane, byte(a>>8))
		}
	}
	if !translucent {
		return rgb, nil
	}
	return rgb, alphaPlane
}

func imageSampleKey(w, h int, rgb, alpha []byte) uint64 {
	sum := fnv.New64a()
	fmt.Fprintf(sum, "%dx%d:", w, h)
	sum.Write(rgb)
	sum.Write(alpha)
	return sum.Sum64()
}

func pdfDeflate(data []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}

// buildObjects serializes every embedded picture and returns the objects
// plus a per-name resource fragment ("/Im1 7 0 R /Im2 9 0 R").
func (d *PDFDoc) buildImageObjects(nextID *int) ([]pdfObject, map[string]int) {
	if len(d.images) == 0 {
		return nil, nil
	}
	ids := make(map[string]int, len(d.images))
	var objs []pdfObject
	for _, im := range d.images {
		if im.alpha != nil {
			im.maskID = *nextID
			*nextID++
		}
		im.id = *nextID
		*nextID++
		ids[im.name] = im.id
	}
	for _, im := range d.images {
		if im.alpha != nil {
			var mask bytes.Buffer
			fmt.Fprintf(&mask, "<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
				"/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode /Length %d >>\nstream\n",
				im.w, im.h, len(im.alpha))
			mask.Write(im.alpha)
			mask.WriteString("\nendstream")
			objs = append(objs, pdfObject{im.maskID, mask.Bytes()})
		}
		var obj bytes.Buffer
		fmt.Fprintf(&obj, "<< /Type /XObject /Subtype /Image /Width %d /Height %d "+
			"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", im.w, im.h)
		if im.alpha != nil {
			fmt.Fprintf(&obj, " /SMask %d 0 R", im.maskID)
		}
		fmt.Fprintf(&obj, " /Length %d >>\nstream\n", len(im.rgb))
		obj.Write(im.rgb)
		obj.WriteString("\nendstream")
		objs = append(objs, pdfObject{im.id, obj.Bytes()})
	}
	return objs, ids
}

// xobjectResources renders a page's /XObject dictionary entries.
func xobjectResources(names []string, ids map[string]int) string {
	if len(names) == 0 {
		return ""
	}
	var out bytes.Buffer
	for _, name := range names {
		id, ok := ids[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&out, "/%s %d 0 R ", name, id)
	}
	return out.String()
}

// useImage records that this page references the named XObject. Order is
// first use, and repeats are ignored — a watermark drawn once per page
// still names its resource once per page.
func (p *pdfPage) useImage(name string) {
	for _, existing := range p.imageNames {
		if existing == name {
			return
		}
	}
	p.imageNames = append(p.imageNames, name)
}
