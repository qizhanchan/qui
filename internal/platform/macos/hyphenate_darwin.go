//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
static int quiHyphenBefore(const UInt16 *chars, int length, int before, const char *language) {
 CFStringRef string=CFStringCreateWithCharacters(NULL,chars,length);
 CFStringRef name=CFStringCreateWithCString(NULL,language,kCFStringEncodingUTF8);
 CFLocaleRef locale=CFLocaleCreate(NULL,name);
 UTF32Char hyphen=0;
 CFIndex position=CFStringGetHyphenationLocationBeforeIndex(string,before,CFRangeMake(0,length),0,locale,&hyphen);
 CFRelease(locale);CFRelease(name);CFRelease(string);
 return (int)position;
}
*/
import "C"
import (
	"unicode/utf16"
	"unsafe"
)

// HyphenPoints returns dictionary hyphenation offsets as rune indices.
func HyphenPoints(word, language string) []int {
	chars := utf16.Encode([]rune(word))
	if len(chars) < 5 || len(chars) > 256 {
		return nil
	}
	if language == "" {
		language = "en"
	}
	// Bound the locale identifier passed to CoreFoundation.
	if len(language) > 64 {
		return nil
	}
	for _, r := range language {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_' || r >= '0' && r <= '9') {
			return nil
		}
	}
	lang := append([]byte(language), 0)
	var points []int
	for before := len(chars); before > 1; {
		pos := int(C.quiHyphenBefore((*C.UInt16)(unsafe.Pointer(&chars[0])), C.int(len(chars)), C.int(before), (*C.char)(unsafe.Pointer(&lang[0]))))
		if pos < 2 || pos >= before {
			break
		}
		if len(chars)-pos >= 2 {
			points = append(points, len(utf16.Decode(chars[:pos])))
		}
		before = pos
	}
	return points
}
