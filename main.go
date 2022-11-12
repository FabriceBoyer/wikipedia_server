package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path"
	"wikipedia_server/wikipedia"

	"github.com/d4l3k/wikigopher/wikitext"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
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
	myRouter := mux.NewRouter().StrictSlash(true)
	myRouter.HandleFunc("/", handleHomePage)
	myRouter.HandleFunc("/search/{id}", errorHandler(handleSearch))
	myRouter.HandleFunc("/page/{id}", errorHandler(handlePage))

	log.Fatal(http.ListenAndServe(":9095", myRouter))
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

type statusError int

func (s statusError) Error() string {
	return fmt.Sprintf("%d - %s", int(s), http.StatusText(int(s)))
}

func errorHandler(f func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			cause := errors.Cause(err)
			status := http.StatusInternalServerError
			if cause, ok := cause.(statusError); ok {
				status = int(cause)
			}

			w.WriteHeader(status)
		}
	}
}
