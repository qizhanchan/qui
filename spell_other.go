//go:build !darwin || !cgo

package qui

// No native spell checker off macOS (or without cgo); there is no embedded
// wordlist. Apps can still install their own via SetSpellChecker.
type noSpellChecker struct{}

func (noSpellChecker) Available() bool { return false }
func (noSpellChecker) Check(string, string) ([]SpellRange, error) {
	return nil, ErrSpellUnavailable
}
func (noSpellChecker) Suggestions(string, string) []string { return nil }
func (noSpellChecker) Learn(string)                        {}
func (noSpellChecker) Unlearn(string)                      {}
func (noSpellChecker) HasLearned(string) bool              { return false }

func platformSpellChecker() SpellChecker { return noSpellChecker{} }
