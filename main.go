package main

import (
	"encoding/json"
	"log"
	"net/http"
	"path"
	"wikipedia_server/utils"
	"wikipedia_server/wikipedia"

	"github.com/d4l3k/wikigopher/wikitext"
	"github.com/gorilla/mux"
)

var root_path = utils.GetEnv("DUMP_PATH", "./dump/")
var wiki = wikipedia.CreateWiki(root_path, "enwiki-pages-articles-multistream-index.txt.bz2", "enwiki-pages-articles-multistream.xml.bz2")
var dict = wikipedia.CreateWiki(root_path, "enwiktionary-pages-articles-multistream-index.txt.bz2", "enwiktionary-pages-articles-multistream.xml.bz2")

// TODO wikibooks, wikisource, wikiversity, wikimedia commons, wikidata, commons, ...

func main() {

	err := wiki.LoadIndex(-1)
	if err != nil {
		panic(err)
	}

	err = dict.LoadIndex(-1)
	if err != nil {
		panic(err)
	}

	handleRequests()
}

func handleRequests() {
	router := mux.NewRouter().StrictSlash(true)
	router.Handle("/", http.FileServer(http.Dir("./static")))
	router.HandleFunc("/search", utils.ErrorHandler(handleSearch))
	router.HandleFunc("/wiki", utils.ErrorHandler(handleWiki))
	router.HandleFunc("/dict", utils.ErrorHandler(handleDict))

	log.Fatal(http.ListenAndServe(":9095", router))
}

func handleSearch(w http.ResponseWriter, r *http.Request) error {
	key := r.URL.Query().Get("name")

	titles, err := wiki.SearchTitles(key)
	if err != nil {
		return err
	}

	err = json.NewEncoder(w).Encode(titles)
	if err != nil {
		return err
	}

	return nil
}

func handleWiki(w http.ResponseWriter, r *http.Request) error {
	return handlePage(wiki, w, r)
}

func handleDict(w http.ResponseWriter, r *http.Request) error {
	return handlePage(dict, w, r)
}

func handlePage(mu *wikipedia.Wiki, w http.ResponseWriter, r *http.Request) error {
	articleName := r.URL.Query().Get("page")

	p, err := mu.GetArticle(articleName)
	if err != nil {
		return err
	}

	if p.Title != articleName {
		http.Redirect(w, r, path.Join("/wiki/", wikitext.TitleToURL(p.Title)), http.StatusTemporaryRedirect)
		return nil
	}

	_, err = w.Write([]byte(p.Text))
	if err != nil {
		return err
	}

	return nil
}
