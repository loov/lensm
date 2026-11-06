package gui

import (
	"os"
	"testing"
)

type named string

func (n named) Name() string { return string(n) }

func TestFilterList(t *testing.T) {
	list := &FilterList[named]{}
	list.SetItems([]named{"main.main", "runtime.Main", "fmt.Println"})
	for filter, want := range map[string]int{
		"":         3,
		"MAIN":     2,
		"main.m":   1,
		"^runtime": 1,
		"(":        1, // invalid regexp keeps the previous list
	} {
		list.SetFilter("^runtime")
		list.SetFilter(filter)
		if len(list.Filtered) != want {
			t.Errorf("filter %q: %d matches, want %d", filter, len(list.Filtered), want)
		}
	}
}

func BenchmarkFilter(b *testing.B) {
	data, err := os.ReadFile("/usr/share/dict/words")
	if err != nil {
		b.Skip(err)
	}
	var items []named
	for i, start := 0, 0; i < len(data); i++ {
		if data[i] == '\n' {
			items = append(items, named(data[start:i]))
			start = i + 1
		}
	}
	list := &FilterList[named]{}
	list.SetItems(items)
	for b.Loop() {
		list.SetFilter("runtime")
	}
}
