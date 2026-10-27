//go:build darwin && cgo

package qui

import (
	"os"
	"reflect"
	"slices"
	"sync"
	"testing"
)

func needSpell(t *testing.T) SpellChecker {
	t.Helper()
	c := SystemSpellChecker()
	if !c.Available() {
		t.Skip("NSSpellChecker has no languages")
	}
	return c
}

func TestSpellCheckDarwin(t *testing.T) {
	c := needSpell(t)
	rs, err := c.Check("I saw teh cat", "en")
	if err != nil {
		t.Fatal(err)
	}
	if want := []SpellRange{{6, 9}}; !reflect.DeepEqual(rs, want) {
		t.Fatalf("got %v want %v", rs, want)
	}
	if rs, err := c.Check("I saw the cat", "en"); err != nil || len(rs) != 0 {
		t.Fatalf("clean text flagged: %v %v", rs, err)
	}
}

func TestSpellCheckDarwinRuneOffsets(t *testing.T) {
	c := needSpell(t)
	// Enough English that NSSpellChecker's script detection does not
	// treat the whole string as Chinese (a bare "teh 中文 teh" is not
	// checked at all).
	rs, err := c.Check("😀 I saw teh cat 中文 and teh dog", "en")
	if err != nil {
		t.Fatal(err)
	}
	// 😀 is 2 UTF-16 units but 1 rune; 中文 1 unit per rune.
	want := []SpellRange{{8, 11}, {23, 26}}
	if !reflect.DeepEqual(rs, want) {
		t.Fatalf("got %v want %v", rs, want)
	}
}

func TestSpellSuggestionsDarwin(t *testing.T) {
	c := needSpell(t)
	if g := c.Suggestions("teh", "en"); !slices.Contains(g, "the") {
		t.Fatalf("suggestions %v lack \"the\"", g)
	}
}

func TestSpellCheckConcurrent(t *testing.T) {
	c := needSpell(t)
	var wg sync.WaitGroup
	errs := make(chan error, 160)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				rs, err := c.Check("I saw teh cat", "en")
				if err != nil {
					errs <- err
					return
				}
				if !reflect.DeepEqual(rs, []SpellRange{{6, 9}}) {
					t.Errorf("got %v", rs)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// Learning mutates the user's real dictionary; opt in explicitly.
func TestSpellLearnDarwin(t *testing.T) {
	if os.Getenv("QUI_SPELL_LEARN_TEST") != "1" {
		t.Skip("set QUI_SPELL_LEARN_TEST=1 to touch the user dictionary")
	}
	c := needSpell(t)
	w := "quixplorbz"
	c.Learn(w)
	defer c.Unlearn(w)
	if !c.HasLearned(w) {
		t.Fatal("HasLearned false after Learn")
	}
	if rs, _ := c.Check("a "+w, "en"); len(rs) != 0 {
		t.Fatalf("learned word flagged: %v", rs)
	}
	c.Unlearn(w)
	if c.HasLearned(w) {
		t.Fatal("HasLearned true after Unlearn")
	}
}
