package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path"
	"wikipedia_server/utils"
	"wikipedia_server/wikipedia"

	"github.com/d4l3k/wikigopher/wikitext"
	"github.com/gorilla/mux"
)

var root_path = "./dump/"

func main() {
	err := wikipedia.LoadIndex(root_path, -1)
	if err != nil {
		panic(err)
	}

	handleRequests()
}

func handleRequests() {
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", handleHomePage)
	router.HandleFunc("/search/{id}", utils.ErrorHandler(handleSearch))
	router.HandleFunc("/page/{id}", utils.ErrorHandler(handlePage))

	log.Fatal(http.ListenAndServe(":9095", router))
}

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
