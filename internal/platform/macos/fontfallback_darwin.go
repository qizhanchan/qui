//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework CoreText -framework CoreFoundation
#include <CoreText/CoreText.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// quiFontForChar asks Core Text which installed font the system UI font
// cascades to for the character in chars[0..n): the font a native text
// view would draw it with. path / name receive the font file and its
// PostScript name (empty when unknown); returns the glyph id, 0 when the
// font has none.
static int quiFontForChar(const UniChar *chars, int n, char *path, int pathLen, char *name, int nameLen) {
	path[0] = 0;
	name[0] = 0;
	CTFontRef base = CTFontCreateUIFontForLanguage(kCTFontUIFontSystem, 12, NULL);
	if (!base) return 0;
	CFStringRef s = CFStringCreateWithCharacters(NULL, chars, n);
	CTFontRef f = CTFontCreateForString(base, s, CFRangeMake(0, n));
	CFRelease(s);
	CFRelease(base);
	if (!f) return 0;
	CGGlyph g[2] = {0, 0};
	int gid = CTFontGetGlyphsForCharacters(f, chars, g, n) ? g[0] : 0;
	CFStringRef ps = CTFontCopyPostScriptName(f);
	if (ps) {
		CFStringGetCString(ps, name, nameLen, kCFStringEncodingUTF8);
		CFRelease(ps);
	}
	CFURLRef url = CTFontCopyAttribute(f, kCTFontURLAttribute);
	if (url) {
		if (!CFURLGetFileSystemRepresentation(url, true, (UInt8 *)path, pathLen)) path[0] = 0;
		CFRelease(url);
	}
	CFRelease(f);
	return gid;
}

// quiFontFileForName fills path with the file of the installed font whose
// PostScript name is name; returns 0 when no such font is installed
// (CTFontCreateWithName substitutes another font then).
static int quiFontFileForName(const char *name, char *path, int pathLen) {
	path[0] = 0;
	CFStringRef want = CFStringCreateWithCString(NULL, name, kCFStringEncodingUTF8);
	if (!want) return 0;
	CTFontRef f = CTFontCreateWithName(want, 12, NULL);
	int ok = 0;
	if (f) {
		CFStringRef got = CTFontCopyPostScriptName(f);
		if (got) {
			ok = CFStringCompare(got, want, 0) == kCFCompareEqualTo;
			CFRelease(got);
		}
		CFURLRef url = ok ? CTFontCopyAttribute(f, kCTFontURLAttribute) : NULL;
		if (url) {
			if (!CFURLGetFileSystemRepresentation(url, true, (UInt8 *)path, pathLen)) path[0] = 0;
			CFRelease(url);
		}
		CFRelease(f);
	}
	CFRelease(want);
	return ok && path[0] != 0;
}

// quiAppend grows *buf (cap *cap) and appends s.
static void quiAppend(char **buf, size_t *len, size_t *cap, const char *s) {
	size_t n = strlen(s);
	if (*len + n + 1 > *cap) {
		size_t nc = (*cap ? *cap : 4096);
		while (*len + n + 1 > nc) nc *= 2;
		char *nb = realloc(*buf, nc);
		if (!nb) return;
		*buf = nb;
		*cap = nc;
	}
	memcpy(*buf + *len, s, n + 1);
	*len += n;
}

static CTFontDescriptorRef quiFamilyDescriptor(CFStringRef family) {
	const void *k[] = {kCTFontFamilyNameAttribute};
	const void *v[] = {family};
	CFDictionaryRef attrs = CFDictionaryCreate(NULL, k, v, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CTFontDescriptorRef d = CTFontDescriptorCreateWithAttributes(attrs);
	CFRelease(attrs);
	return d;
}

// quiFontFamilies lists the installed font families, one per line as
// "name\tflags" (flags: 1 monospaced, 2 draws Han). Hidden system families
// (".SF NS") are left out. The caller frees the result.
static char *quiFontFamilies(void) {
	char *buf = NULL;
	size_t len = 0, cap = 0;
	quiAppend(&buf, &len, &cap, "");
	CFArrayRef names = CTFontManagerCopyAvailableFontFamilyNames();
	if (!names) return buf;
	char name[512], line[600];
	for (CFIndex i = 0; i < CFArrayGetCount(names); i++) {
		CFStringRef fam = CFArrayGetValueAtIndex(names, i);
		if (!CFStringGetCString(fam, name, sizeof name, kCFStringEncodingUTF8) || name[0] == '.' || name[0] == 0) continue;
		CTFontDescriptorRef d = quiFamilyDescriptor(fam);
		CTFontRef f = d ? CTFontCreateWithFontDescriptor(d, 12, NULL) : NULL;
		if (d) CFRelease(d);
		if (!f) continue;
		int flags = 0;
		CFStringRef got = CTFontCopyFamilyName(f);
		int same = got && CFStringCompare(got, fam, 0) == kCFCompareEqualTo;
		if (got) CFRelease(got);
		if (same) {
			if (CTFontGetSymbolicTraits(f) & kCTFontTraitMonoSpace) flags |= 1;
			CFCharacterSetRef cs = CTFontCopyCharacterSet(f);
			if (cs) {
				if (CFCharacterSetIsLongCharacterMember(cs, 0x4E2D)) flags |= 2;
				CFRelease(cs);
			}
			snprintf(line, sizeof line, "%s\t%d\n", name, flags);
			quiAppend(&buf, &len, &cap, line);
		}
		CFRelease(f);
	}
	CFRelease(names);
	return buf;
}

// quiFamilyFaces lists the faces of family, one per line as
// "path\tpostscript\tweight\tflags" (weight is Core Text's -1…1 weight
// trait times 1000; flags: 1 italic, 2 condensed or expanded). The
// caller frees the result.
static char *quiFamilyFaces(const char *family) {
	char *buf = NULL;
	size_t len = 0, cap = 0;
	quiAppend(&buf, &len, &cap, "");
	CFStringRef fam = CFStringCreateWithCString(NULL, family, kCFStringEncodingUTF8);
	if (!fam) return buf;
	CTFontDescriptorRef d = quiFamilyDescriptor(fam);
	CFRelease(fam);
	if (!d) return buf;
	CFArrayRef all = CTFontDescriptorCreateMatchingFontDescriptors(d, NULL);
	CFRelease(d);
	if (!all) return buf;
	char path[4096], ps[512], line[4800];
	for (CFIndex i = 0; i < CFArrayGetCount(all); i++) {
		CTFontDescriptorRef fd = CFArrayGetValueAtIndex(all, i);
		CFStringRef got = CTFontDescriptorCopyAttribute(fd, kCTFontFamilyNameAttribute);
		CFStringRef want = CFStringCreateWithCString(NULL, family, kCFStringEncodingUTF8);
		int same = got && want && CFStringCompare(got, want, 0) == kCFCompareEqualTo;
		if (got) CFRelease(got);
		if (want) CFRelease(want);
		if (!same) continue;
		CFURLRef url = CTFontDescriptorCopyAttribute(fd, kCTFontURLAttribute);
		CFStringRef name = CTFontDescriptorCopyAttribute(fd, kCTFontNameAttribute);
		int ok = url && name &&
			CFURLGetFileSystemRepresentation(url, true, (UInt8 *)path, sizeof path) &&
			CFStringGetCString(name, ps, sizeof ps, kCFStringEncodingUTF8);
		if (url) CFRelease(url);
		if (name) CFRelease(name);
		if (!ok) continue;
		double weight = 0;
		int flags = 0;
		CFDictionaryRef traits = CTFontDescriptorCopyAttribute(fd, kCTFontTraitsAttribute);
		if (traits) {
			CFNumberRef w = CFDictionaryGetValue(traits, kCTFontWeightTrait);
			if (w) CFNumberGetValue(w, kCFNumberDoubleType, &weight);
			CFNumberRef sym = CFDictionaryGetValue(traits, kCTFontSymbolicTrait);
			uint32_t st = 0;
			if (sym) CFNumberGetValue(sym, kCFNumberSInt32Type, &st);
			if (st & kCTFontTraitItalic) flags |= 1;
			if (st & (kCTFontTraitCondensed | kCTFontTraitExpanded)) flags |= 2;
			CFRelease(traits);
		}
		snprintf(line, sizeof line, "%s\t%s\t%d\t%d\n", path, ps, (int)(weight * 1000), flags);
		quiAppend(&buf, &len, &cap, line);
	}
	CFRelease(all);
	return buf;
}
*/
import "C"

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unsafe"
)

// FontFileForRune returns the font file and PostScript name of the
// installed font Core Text falls back to for r from the system UI font
// (what a native text view would draw r with). ok is false when that font
// has no glyph for r. The caller decides what to make of LastResort
// (Apple's "missing glyph" font) and Apple Color Emoji.
func FontFileForRune(r rune) (path, postScript string, ok bool) {
	chars := utf16.Encode([]rune{r})
	if len(chars) == 0 {
		return "", "", false
	}
	var p [4096]C.char
	var n [256]C.char
	gid := C.quiFontForChar((*C.UniChar)(unsafe.Pointer(&chars[0])), C.int(len(chars)), &p[0], C.int(len(p)), &n[0], C.int(len(n)))
	path, postScript = C.GoString(&p[0]), C.GoString(&n[0])
	return path, postScript, gid != 0 && path != ""
}

// FontFileForName returns the file of the installed font with the given
// PostScript name ("PingFangSC-Regular"). Fonts macOS downloads on demand
// live under hashed asset directories, so this is the only reliable way to
// find them.
func FontFileForName(postScript string) (path string, ok bool) {
	cs := C.CString(postScript)
	defer C.free(unsafe.Pointer(cs))
	var p [4096]C.char
	if C.quiFontFileForName(cs, &p[0], C.int(len(p))) == 0 {
		return "", false
	}
	return C.GoString(&p[0]), true
}

// FontFamily is an installed font family as Core Text lists it.
type FontFamily struct {
	Name      string
	Monospace bool // its regular face is fixed-pitch
	Han       bool // it draws Chinese characters
}

// FontFamilies lists the installed font families (hidden system ones such
// as ".SF NS" left out), in Core Text's order (by name).
func FontFamilies() []FontFamily {
	cs := C.quiFontFamilies()
	defer C.free(unsafe.Pointer(cs))
	var out []FontFamily
	for _, line := range strings.Split(C.GoString(cs), "\n") {
		name, flags, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(flags)
		out = append(out, FontFamily{Name: name, Monospace: n&1 != 0, Han: n&2 != 0})
	}
	return out
}

// FamilyFace is one installed face of a font family.
type FamilyFace struct {
	Path, PostScript string
	// Weight is Core Text's weight trait (-1 thin … 0 regular … 1 black).
	Weight    float64
	Italic    bool
	Stretched bool // a condensed or expanded cut
}

// FamilyFaces lists the installed faces of family — also those macOS
// keeps in its downloadable-asset directories, which no font directory
// scan finds.
func FamilyFaces(family string) []FamilyFace {
	cf := C.CString(family)
	defer C.free(unsafe.Pointer(cf))
	cs := C.quiFamilyFaces(cf)
	defer C.free(unsafe.Pointer(cs))
	var out []FamilyFace
	for _, line := range strings.Split(C.GoString(cs), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			continue
		}
		w, _ := strconv.Atoi(f[2])
		n, _ := strconv.Atoi(f[3])
		out = append(out, FamilyFace{Path: f[0], PostScript: f[1], Weight: float64(w) / 1000, Italic: n&1 != 0, Stretched: n&2 != 0})
	}
	return out
}
