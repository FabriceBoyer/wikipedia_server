package main

import (
	"log"
	"net/http"
	"wikipedia_server/wikipedia"

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
	router.HandleFunc("/search/{id}", errorHandler(handleSearch))
	router.HandleFunc("/page/{id}", errorHandler(handlePage))

	log.Fatal(http.ListenAndServe(":9095", router))
}
