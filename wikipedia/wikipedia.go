package wikipedia

import (
	"bufio"
	"compress/bzip2"
	"encoding/xml"
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

type indexEntry struct {
	id, seek int
}

type Wiki struct {
	indexFile    string
	articlesFile string
	sync.Mutex

	offsets    map[uint64]indexEntry
	offsetSize map[int]int
}

func CreateWiki(root_path string, indexFile string, articlesFile string) *Wiki {
	return &Wiki{
		indexFile:    filepath.Join(root_path, indexFile),
		articlesFile: filepath.Join(root_path, articlesFile),
		offsets:      map[uint64]indexEntry{},
		offsetSize:   map[int]int{},
	}
}

// TODO read redirect from content and modify index to it
func (mu *Wiki) LoadIndex(limit int) error {

	f, err := os.Open(mu.indexFile)
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

	log.Printf("Reading index file %v ...", mu.indexFile)
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

type Redirect struct {
	Title string `xml:"title,attr"`
}

type Page struct {
	XMLName    xml.Name   `xml:"page"`
	Title      string     `xml:"title"`
	NS         int        `xml:"ns"`
	ID         int        `xml:"id"`
	Redirect   []Redirect `xml:"redirect"`
	RevisionID string     `xml:"revision>id"`
	Timestamp  string     `xml:"revision>timestamp"`
	Username   string     `xml:"revision>contributor>username"`
	UserID     string     `xml:"revision>contributor>id"`
	Model      string     `xml:"revision>model"`
	Format     string     `xml:"revision>format"`
	Text       string     `xml:"revision>text"`
}

func (mu *Wiki) SearchTitles(key string) ([]string, error) {
	return []string{}, nil
}

func (mu *Wiki) GetArticle(name string) (Page, error) {
	articleMeta, err := mu.fetchArticle(name)
	if err != nil {
		return Page{}, err
	}

	p, err := mu.readArticle(articleMeta)
	if err != nil {
		return Page{}, err
	}

	return p, nil
}

func (mu *Wiki) readArticle(meta indexEntry) (Page, error) {
	f, err := os.Open(mu.articlesFile)
	if err != nil {
		return Page{}, err
	}
	defer f.Close()

	mu.Lock()
	maxTries := mu.offsetSize[meta.seek]
	mu.Unlock()

	r := bzip2.NewReader(f)

	if _, err := f.Seek(int64(meta.seek), 0); err != nil {
		return Page{}, err
	}

	d := xml.NewDecoder(r)

	var p Page
	for i := 0; i < maxTries; i++ {
		if err := d.Decode(&p); err != nil {
			return Page{}, err
		}
		if p.ID == meta.id {
			return p, nil
		}
	}

	return Page{}, errors.Errorf("failed to find page after %d tries", maxTries)
}

func (mu *Wiki) fetchArticle(name string) (indexEntry, error) {
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
