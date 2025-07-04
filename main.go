
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/spaolacci/murmur3"
)

var (
	db *badger.DB

	adminUser     = getEnv("ADMIN_USERNAME", "admin")
	adminPassword = getEnv("ADMIN_PASSWORD", "password") // In a real application, use hashed passwords!

	sessions     = make(map[string]time.Time)
	sessionsMutex sync.Mutex
	cookieName   = "session_token"
	sessionExpiry = 10 * time.Minute

	baseURL = getEnv("BASE_URL", "http://localhost:8080")
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
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/admin", handleAdmin)

	http.HandleFunc("/count", authMiddleware(handleCount))
	http.HandleFunc("/mappings", authMiddleware(handleMappings))

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

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == adminUser && password == adminPassword {
		sessionToken, err := generateSessionToken()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		sessionsMutex.Lock()
		sessions[sessionToken] = time.Now().Add(sessionExpiry)
		sessionsMutex.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:    cookieName,
			Value:   sessionToken,
			Expires: time.Now().Add(sessionExpiry),
			Path:    "/",
			HttpOnly: true,
		})

		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?error=1", http.StatusSeeOther)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err == nil {
		sessionsMutex.Lock()
		delete(sessions, c.Value)
		sessionsMutex.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:    cookieName,
		Value:   "",
		Expires: time.Now().AddDate(-1, 0, 0), // Expire immediately
		Path:    "/",
		HttpOnly: true,
	})

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}

		sessionsMutex.Lock()
		expiry, ok := sessions[c.Value]
		sessionsMutex.Unlock()

		if !ok || expiry.Before(time.Now()) {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}

		// Update session expiry
		sessionsMutex.Lock()
		sessions[c.Value] = time.Now().Add(sessionExpiry)
		sessionsMutex.Unlock()

		next.ServeHTTP(w, r)
	}
}

func handleAdmin(w http.ResponseWriter, r *http.Request) {
	// Check if authenticated
	c, err := r.Cookie(cookieName)
	isAuthenticated := false
	if err == nil {
		sessionsMutex.Lock()
		expiry, ok := sessions[c.Value]
		sessionsMutex.Unlock()
		if ok && expiry.After(time.Now()) {
			isAuthenticated = true
		}
	}

	data := struct {
		IsAuthenticated bool
		Error           bool
	}{
		IsAuthenticated: isAuthenticated,
		Error:           r.URL.Query().Get("error") == "1",
	}

	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, data)
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

	fmt.Fprintf(w, "Shortened URL: %s/s/%s", baseURL, shortURL)
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

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// getEnv reads an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
