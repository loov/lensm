package gui

import "testing"

func TestFuzzyScore(t *testing.T) {
	if _, ok := FuzzyScore("xyz", "loov.dev/lensm/internal/goobj.layoutJumps"); ok {
		t.Error("xyz matched")
	}
	if _, ok := FuzzyScore("LJ", "goobj.layoutJumps"); !ok {
		t.Error("case-insensitive match failed")
	}
	better, _ := FuzzyScore("lj", "loov.dev/lensm/internal/goobj.layoutJumps")
	worse, ok := FuzzyScore("lj", "loov.dev/lensm/jumps.Load")
	if !ok || better <= worse {
		t.Errorf("layoutJumps scored %d, jumps.Load %d", better, worse)
	}
	short, _ := FuzzyScore("main", "main.main")
	long, _ := FuzzyScore("main", "main.mainLoop")
	if short <= long {
		t.Errorf("main.main scored %d, main.mainLoop %d", short, long)
	}
}

func TestShortName(t *testing.T) {
	if got := ShortName("loov.dev/lensm/internal/goobj.(*Code).layoutJumps"); got != "goobj.(*Code).layoutJumps" {
		t.Errorf("got %q", got)
	}
}

func TestSplitPackage(t *testing.T) {
	for _, tc := range []struct{ name, pkg, rest string }{
		{"loov.dev/lensm/internal/goobj.(*Code).layoutJumps", "loov.dev/lensm/internal/goobj.", "(*Code).layoutJumps"},
		{"main.main", "main.", "main"},
		{"slices.Sort[go.shape.[]string]", "slices.", "Sort[go.shape.[]string]"},
		{"_start", "", "_start"},
	} {
		pkg, rest := SplitPackage(tc.name)
		if pkg != tc.pkg || rest != tc.rest {
			t.Errorf("SplitPackage(%q) = %q, %q", tc.name, pkg, rest)
		}
	}
}
