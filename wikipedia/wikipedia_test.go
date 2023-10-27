package wikipedia

import (
	"fmt"
	"testing"

	"github.com/fabriceboyer/common_go_utils/utils"
	"github.com/pkg/profile"
	"github.com/spf13/viper"
)

func createDefaultWiki() *Wiki {
	utils.SetupTestConfig()
	return CreateWiki(viper.GetString("DUMP_PATH"), "enwiki-pages-articles-multistream-index.txt.bz2", "enwiki-pages-articles-multistream.xml.bz2")
}
func TestWikipedia(t *testing.T) {
	mu := createDefaultWiki()

	err := mu.LoadIndex(1e4)
	if err != nil {
		t.Error(err)
	}

	p, err := mu.GetArticle("Anarchism")
	if err != nil {
		t.Error(err)
	}

	fmt.Print(p.Text + "\n")
}

func BenchmarkWikipedia(t *testing.B) {
	defer profile.Start(profile.MemProfileAllocs).Stop()
	mu := createDefaultWiki()

	err := mu.LoadIndex(1e6)
	if err != nil {
		t.Error(err)
	}
}
