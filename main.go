
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/dgraph-io/badger/v3"
	"github.com/spaolacci/murmur3"
)

var (
	db *badger.DB
)

func main() {
	var err error
	db, err = badger.Open(badger.DefaultOptions("/tmp/badger"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/shorten", handleShorten)
	http.HandleFunc("/s/", handleRedirect)
	http.HandleFunc("/count", handleCount)
	http.HandleFunc("/mappings", handleMappings)

	fmt.Println("Server started at :8080")
	http.ListenAndServe(":8080", nil)
}

func handleCount(w http.ResponseWriter, r *http.Request) {
	count := 0
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}
		return nil
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Total URLs stored: %d\n", count)
}

func handleMappings(w http.ResponseWriter, r *http.Request) {
	mappings := make(map[string]string)
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				mappings[string(k)] = string(v)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mappings)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func handleShorten(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	originalURL := r.FormValue("url")
	if originalURL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	shortURL := generateShortURL(originalURL)

	err := db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(shortURL), []byte(originalURL))
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Shortened URL: http://localhost:8080/s/%s", shortURL)
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	shortURL := r.URL.Path[len("/s/"):]
	if shortURL == "" {
		http.NotFound(w, r)
		return
	}

	var originalURL []byte
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(shortURL))
		if err != nil {
			return err
		}
		originalURL, err = item.ValueCopy(nil)
		return err
	})

	if err != nil {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, string(originalURL), http.StatusFound)
}

func generateShortURL(data string) string {
	hasher := murmur3.New128()
	hasher.Write([]byte(data))
	return hex.EncodeToString(hasher.Sum(nil))[:8]
}
