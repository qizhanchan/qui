//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// One spell-document tag for the whole process: "Ignore" state is kept by
// the app (document scoped), so a single NSSpellChecker document suffices.
static NSInteger quiSpellTag(void) {
	static NSInteger tag;
	static dispatch_once_t once;
	dispatch_once(&once, ^{ tag = [NSSpellChecker uniqueSpellDocumentTag]; });
	return tag;
}

static NSString *quiNS16(const uint16_t *s, int n) {
	return [NSString stringWithCharacters:(const unichar *)s length:(NSUInteger)(n > 0 ? n : 0)];
}

static NSString *quiLang(const char *l) {
	return (l && l[0]) ? [NSString stringWithUTF8String:l] : nil;
}

int quiSpellAvailable(void) {
	@autoreleasepool {
		NSSpellChecker *sc = [NSSpellChecker sharedSpellChecker];
		return sc != nil && sc.availableLanguages.count > 0;
	}
}

// quiSpellCheck checks n UTF-16 units and stores malloc'd (location,
// length) int32 pairs in *out. Returns the pair count, or -1 on timeout.
// With an installed language it checks synchronously in that language.
// Without one it lets the checker identify the language: synchronously on
// the main thread, elsewhere through the asynchronous request API, waiting
// (2s) for the completion handler.
// quiSpellHasLanguage reports whether the checker has a dictionary for l
// itself or for one of its regional variants ("en" → "en_GB").
static BOOL quiSpellHasLanguage(NSSpellChecker *sc, NSString *l) {
	NSString *prefix = [l stringByAppendingString:@"_"];
	for (NSString *a in sc.availableLanguages) {
		if ([a isEqualToString:l] || [a hasPrefix:prefix]) return YES;
	}
	return NO;
}

// quiSpellCheckLanguage finds every misspelling of str in exactly language
// l. It exists because checkString / requestCheckingOfString honor
// neither the orthography option nor setLanguage: while the checker
// automatically identifies languages (the default) — they resolve "en"
// to the user's regional dictionary instead (en_SG on an en-SG system,
// whose dictionary accepts "teh"). checkSpellingOfString:…language: takes
// the language per call and leaves the shared checker's state alone.
static NSArray<NSValue *> *quiSpellCheckLanguage(NSSpellChecker *sc, NSString *str, NSString *l) {
	NSMutableArray<NSValue *> *found = [NSMutableArray array];
	NSInteger at = 0, n = (NSInteger)str.length;
	while (at < n) {
		NSRange r = [sc checkSpellingOfString:str startingAt:at language:l wrap:NO
		               inSpellDocumentWithTag:quiSpellTag() wordCount:NULL];
		if (r.location == NSNotFound || r.length == 0 || (NSInteger)r.location < at) break;
		[found addObject:[NSValue valueWithRange:r]];
		at = (NSInteger)NSMaxRange(r);
	}
	return found;
}

int quiSpellCheck(const uint16_t *s, int n, const char *lang, int32_t **out) {
	*out = NULL;
	if (n <= 0) return 0;
	@autoreleasepool {
		NSSpellChecker *sc = [NSSpellChecker sharedSpellChecker];
		NSString *str = quiNS16(s, n);
		NSString *l = quiLang(lang);
		if (l && quiSpellHasLanguage(sc, l)) {
			NSArray<NSValue *> *found = quiSpellCheckLanguage(sc, str, l);
			if (found.count == 0) return 0;
			int32_t *buf = malloc(sizeof(int32_t) * 2 * found.count);
			for (NSUInteger i = 0; i < found.count; i++) {
				NSRange r = found[i].rangeValue;
				buf[2*i] = (int32_t)r.location;
				buf[2*i+1] = (int32_t)r.length;
			}
			*out = buf;
			return (int)found.count;
		}
		// No language, or none installed for it: let the checker identify
		// the language itself.
		NSDictionary *opts = nil;
		if (l) {
			NSOrthography *o = [NSOrthography defaultOrthographyForLanguage:l];
			if (o) opts = @{NSTextCheckingOrthographyKey: o};
		}
		NSRange all = NSMakeRange(0, str.length);
		NSArray<NSTextCheckingResult *> *results;
		if ([NSThread isMainThread]) {
			results = [sc checkString:str range:all types:NSTextCheckingTypeSpelling options:opts
			   inSpellDocumentWithTag:quiSpellTag() orthography:NULL wordCount:NULL];
		} else {
			dispatch_semaphore_t done = dispatch_semaphore_create(0);
			NSMutableArray *box = [NSMutableArray array];
			[sc requestCheckingOfString:str range:all types:NSTextCheckingTypeSpelling options:opts
			     inSpellDocumentWithTag:quiSpellTag()
			          completionHandler:^(NSInteger seq, NSArray<NSTextCheckingResult *> *res, NSOrthography *o, NSInteger wc) {
				@synchronized (box) { if (res) [box addObjectsFromArray:res]; }
				dispatch_semaphore_signal(done);
			}];
			if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC)) != 0) return -1;
			@synchronized (box) { results = [box copy]; }
		}
		int count = 0;
		for (NSTextCheckingResult *r in results) if (r.resultType == NSTextCheckingTypeSpelling) count++;
		if (count == 0) return 0;
		int32_t *buf = malloc(sizeof(int32_t) * 2 * count);
		int i = 0;
		for (NSTextCheckingResult *r in results) {
			if (r.resultType != NSTextCheckingTypeSpelling) continue;
			buf[2*i] = (int32_t)r.range.location;
			buf[2*i+1] = (int32_t)r.range.length;
			i++;
		}
		*out = buf;
		return count;
	}
}

char *quiSpellGuesses(const uint16_t *w, int n, const char *lang) {
	@autoreleasepool {
		NSString *word = quiNS16(w, n);
		NSArray<NSString *> *g = [[NSSpellChecker sharedSpellChecker] guessesForWordRange:NSMakeRange(0, word.length)
			inString:word language:quiLang(lang) inSpellDocumentWithTag:quiSpellTag()];
		if (g.count == 0) return NULL;
		return strdup([[g componentsJoinedByString:@"\n"] UTF8String]);
	}
}

void quiSpellLearn(const uint16_t *w, int n) {
	@autoreleasepool { [[NSSpellChecker sharedSpellChecker] learnWord:quiNS16(w, n)]; }
}

void quiSpellUnlearn(const uint16_t *w, int n) {
	@autoreleasepool { [[NSSpellChecker sharedSpellChecker] unlearnWord:quiNS16(w, n)]; }
}

int quiSpellHasLearned(const uint16_t *w, int n) {
	@autoreleasepool { return [[NSSpellChecker sharedSpellChecker] hasLearnedWord:quiNS16(w, n)]; }
}
