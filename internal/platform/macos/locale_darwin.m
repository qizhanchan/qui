//go:build darwin

#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

// quiPreferredLocale returns the first entry of the user's ordered
// preferred-language list as a BCP-47 tag ("zh-Hans-CN", "ar", "en-US").
// Caller owns the returned buffer and must free() it.
//
// +preferredLanguages, not +currentLocale: the former is the UI language
// the user asked for, the latter is the region/formatting locale. They
// differ routinely (an English UI with German number formats), and the
// widget text we are translating follows the UI language.
char *quiPreferredLocale(void) {
    @autoreleasepool {
        NSArray<NSString *> *langs = [NSLocale preferredLanguages];
        if (langs.count == 0) {
            return NULL;
        }
        const char *utf8 = [langs[0] UTF8String];
        if (utf8 == NULL) {
            return NULL;
        }
        return strdup(utf8);
    }
}
