package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"wikipedia_server/wikipedia"

	"github.com/d4l3k/wikigopher/wikitext"
	"github.com/gorilla/mux"
)

func handleHomePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Welcome to wikipedia server, please use API")
}

func handleSearch(w http.ResponseWriter, r *http.Request) error {
	vars := mux.Vars(r)
	key := vars["id"]

	titles, err := wikipedia.SearchTitles(key)
	if err != nil {
		return err
	}

	err = json.NewEncoder(w).Encode(titles)
	if err != nil {
		return err
	}

	return nil
}

func handlePage(w http.ResponseWriter, r *http.Request) error {
	//articleName := wikitext.URLToTitle(path.Base(r.URL.Path))
	vars := mux.Vars(r)
	articleName := vars["id"]

	p, err := wikipedia.GetArticle(articleName, root_path)
	if err != nil {
		return err
	}

	if p.Title != articleName {
		http.Redirect(w, r, path.Join("/page/", wikitext.TitleToURL(p.Title)), http.StatusTemporaryRedirect)
		return nil
	}

	_, err = w.Write([]byte(p.Text))
	if err != nil {
		return err
	}

	return nil
}
