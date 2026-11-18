//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CoreText
#include <CoreFoundation/CoreFoundation.h>
#include <CoreText/CoreText.h>
static double quiSystemAdvance(const UInt16 *chars,int length,const UInt16 *name,int nameLength,double size,double weight,double opticalSize,int italic) {
 CFStringRef family=CFStringCreateWithCharacters(NULL,name,nameLength);
 CTFontRef font;
 if(nameLength>=6&&name[0]=='.'&&name[1]=='S'&&name[2]=='F'&&name[3]=='N'&&name[4]=='S'&&name[5]=='-') font=CTFontCreateUIFontForLanguage(kCTFontUIFontSystem,size,NULL);
 else font=CTFontCreateWithName(family,size,NULL);
 CFRelease(family);
 if(!font)return -1;
 if(italic){CTFontRef slanted=CTFontCreateCopyWithSymbolicTraits(font,size,NULL,kCTFontItalicTrait,kCTFontItalicTrait);if(slanted){CFRelease(font);font=slanted;}}
 int tags[2]={0x77676874,0x6f70737a};double values[2]={weight,opticalSize};
 CFMutableDictionaryRef variations=CFDictionaryCreateMutable(NULL,2,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 for(int i=0;i<2;i++){CFNumberRef k=CFNumberCreate(NULL,kCFNumberIntType,&tags[i]);CFNumberRef v=CFNumberCreate(NULL,kCFNumberDoubleType,&values[i]);CFDictionarySetValue(variations,k,v);CFRelease(k);CFRelease(v);}
 const void *keys[]={kCTFontVariationAttribute};const void *vals[]={variations};
 CFDictionaryRef descriptorAttrs=CFDictionaryCreate(NULL,keys,vals,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CTFontDescriptorRef descriptor=CTFontDescriptorCreateWithAttributes(descriptorAttrs);
 CTFontRef instance=CTFontCreateCopyWithAttributes(font,size,NULL,descriptor);
 CFRelease(descriptor);CFRelease(descriptorAttrs);CFRelease(variations);CFRelease(font);
 CFStringRef string=CFStringCreateWithCharacters(NULL,chars,length);
 const void *attrKeys[]={kCTFontAttributeName};const void *attrVals[]={instance};
 CFDictionaryRef attrs=CFDictionaryCreate(NULL,attrKeys,attrVals,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFAttributedStringRef attributed=CFAttributedStringCreate(NULL,string,attrs);
 CTLineRef line=CTLineCreateWithAttributedString(attributed);
 double advance=CTLineGetTypographicBounds(line,NULL,NULL,NULL);
 CFRelease(line);CFRelease(attributed);CFRelease(attrs);CFRelease(string);CFRelease(instance);
 return advance;
}
*/
import "C"
import (
	"math"
	"unicode/utf16"
	"unsafe"
)

// SystemTextAdvance measures a Core Text run with weight and optical-size axes.
// It returns false when the request cannot be measured by the native bridge.
func SystemTextAdvance(text []rune, name string, size, weight, opticalSize float32, italic bool) (float32, bool) {
	if size <= 0 || size > 65536 || math.IsNaN(float64(size)) || math.IsInf(float64(size), 0) {
		return 0, false
	}
	if len(text) == 0 {
		return 0, true
	}
	if len(text) > 65536 || len(name) > 256 {
		return 0, false
	}
	chars := utf16.Encode(text)
	family := utf16.Encode([]rune(name))
	if len(family) == 0 {
		return 0, false
	}
	for _, value := range []float32{weight, opticalSize} {
		if value <= 0 || value > 65536 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return 0, false
		}
	}
	italicFlag := C.int(0)
	if italic {
		italicFlag = 1
	}
	width := float64(C.quiSystemAdvance((*C.UInt16)(unsafe.Pointer(&chars[0])), C.int(len(chars)), (*C.UInt16)(unsafe.Pointer(&family[0])), C.int(len(family)), C.double(size), C.double(weight), C.double(opticalSize), italicFlag))
	return float32(width), width >= 0 && !math.IsNaN(width) && !math.IsInf(width, 0)
}
