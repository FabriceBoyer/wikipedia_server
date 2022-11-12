package wikipedia

import (
	"fmt"
	"testing"
)

func TestWikipedia(t *testing.T) {
	root_path := "../dump/"

	err := LoadIndex(root_path, 1e4)
	if err != nil {
		t.Error(err)
	}

	p, err := GetArticle("Anarchism", root_path)
	if err != nil {
		t.Error(err)
	}

	fmt.Print(p.Text + "\n")
}
