package wikipedia

import (
	"bufio"
	"compress/bzip2"
	"encoding/xml"
	"flag"
	"log"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/creachadair/cityhash"
	"github.com/d4l3k/go-pbzip2"
	"github.com/pkg/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	indexFile    = flag.String("index", "enwiki-pages-articles-multistream-index.txt.bz2", "the index file to load")
	articlesFile = flag.String("articles", "enwiki-pages-articles-multistream.xml.bz2", "the article dump file to load")
)

type indexEntry struct {
	id, seek int
}

var mu = struct {
	sync.Mutex

	offsets    map[uint64]indexEntry
	offsetSize map[int]int
}{
	offsets:    map[uint64]indexEntry{},
	offsetSize: map[int]int{},
}

func LoadIndex(root_path string, limit int) error {

	f, err := os.Open(filepath.Join(root_path, *indexFile))
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := pbzip2.NewReader(f)
	if err != nil {
		return err
	}
	defer r.Close()

	scanner := bufio.NewScanner(r)

	log.Printf("Reading index file...")
	i := 0
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 3 {
			return errors.Errorf("expected at least 3 parts, got: %#v", parts)
		}
		seek, err := strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		id, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}
		title := strings.Join(parts[2:], ":")
		entry := indexEntry{
			id:   id,
			seek: seek,
		}
		titleHash := cityhash.Hash64([]byte(title))

		mu.Lock()
		mu.offsets[titleHash] = entry
		mu.offsetSize[entry.seek]++
		mu.Unlock()

		i++
		if i%100000 == 0 {
			log.Printf("read %d entries", i)
		}
		if limit > 0 && i >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	log.Printf("Done reading!")

	return nil
}

type redirect struct {
	Title string `xml:"title,attr"`
}

type page struct {
	XMLName    xml.Name   `xml:"page"`
	Title      string     `xml:"title"`
	NS         int        `xml:"ns"`
	ID         int        `xml:"id"`
	Redirect   []redirect `xml:"redirect"`
	RevisionID string     `xml:"revision>id"`
	Timestamp  string     `xml:"revision>timestamp"`
	Username   string     `xml:"revision>contributor>username"`
	UserID     string     `xml:"revision>contributor>id"`
	Model      string     `xml:"revision>model"`
	Format     string     `xml:"revision>format"`
	Text       string     `xml:"revision>text"`
}

func SearchTitles(key string) ([]string, error) {
	return []string{}, nil
}

func GetArticle(name string, root_path string) (page, error) {
	articleMeta, err := fetchArticle(name)
	if err != nil {
		return page{}, err
	}

	p, err := readArticle(articleMeta, root_path)
	if err != nil {
		return page{}, err
	}

	return p, nil
}

func readArticle(meta indexEntry, root_path string) (page, error) {
	f, err := os.Open(filepath.Join(root_path, *articlesFile))
	if err != nil {
		return page{}, err
	}
	defer f.Close()

	mu.Lock()
	maxTries := mu.offsetSize[meta.seek]
	mu.Unlock()

	r := bzip2.NewReader(f)

	if _, err := f.Seek(int64(meta.seek), 0); err != nil {
		return page{}, err
	}

	d := xml.NewDecoder(r)

	var p page
	for i := 0; i < maxTries; i++ {
		if err := d.Decode(&p); err != nil {
			return page{}, err
		}
		if p.ID == meta.id {
			return p, nil
		}
	}

	return page{}, errors.Errorf("failed to find page after %d tries", maxTries)
}

func fetchArticle(name string) (indexEntry, error) {
	mu.Lock()
	defer mu.Unlock()

	articleMeta, ok := mu.offsets[cityhash.Hash64([]byte(name))]
	if ok {
		return articleMeta, nil
	}
	caser := cases.Title(language.AmericanEnglish)
	articleMeta, ok = mu.offsets[cityhash.Hash64([]byte(caser.String(strings.ToLower(name))))]
	if ok {
		return articleMeta, nil
	}
	return indexEntry{}, errors.Errorf("article not found: %q", name)
}
