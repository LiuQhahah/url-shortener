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
	"strconv"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/mssola/user_agent"
	"github.com/spaolacci/murmur3"
)

type URLData struct {
	OriginalURL string `json:"original_url"`
	Count       int    `json:"count"`
	Device      string `json:"device,omitempty"`
	OS          string `json:"os,omitempty"`
}

type MappingsResponse struct {
	Mappings   []MappingEntry `json:"mappings"`
	TotalCount int            `json:"total_count"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

type MappingEntry struct {
	ShortURL string `json:"short_url"`
	URLData
}

var (
	db *badger.DB

	adminUser     = getEnv("ADMIN_USERNAME", "admin")
	adminPassword = getEnv("ADMIN_PASSWORD", "password") // In a real application, use hashed passwords!

	sessions      = make(map[string]time.Time)
	sessionsMutex sync.Mutex
	cookieName    = "session_token"
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
	http.HandleFunc("/mappings-api", authMiddleware(handleMappingsAPI))
	http.HandleFunc("/admin/mappings", authMiddleware(handleAdminMappingsPage))
	http.HandleFunc("/mock-shorten", authMiddleware(handleMockShorten))

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

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

func handleMappingsAPI(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(pageSizeStr)
	if err != nil || pageSize < 1 || pageSize > 1000 { // Limit page size to prevent abuse
		pageSize = 100
	}

	offset := (page - 1) * pageSize
	limit := pageSize

	var paginatedMappings []MappingEntry
	totalCount := 0
	currentCount := 0

	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()

		// First pass to get total count
		for it.Rewind(); it.Valid(); it.Next() {
			totalCount++
		}

		// Second pass for paginated data
		it.Rewind()
		for i := 0; i < offset; i++ {
			if !it.Valid() {
				break
			}
			it.Next()
		}

		for it.Valid() && currentCount < limit {
			item := it.Item()
			k := item.Key()
			valCopy, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			var urlData URLData
			jsonErr := json.Unmarshal(valCopy, &urlData)
			if jsonErr != nil { // If unmarshaling fails, assume it's an old plain string
				urlData = URLData{
					OriginalURL: string(valCopy),
					Count:       0,
				}
			}
			paginatedMappings = append(paginatedMappings, MappingEntry{
				ShortURL: string(k),
				URLData:  urlData,
			})
			currentCount++
			it.Next()
		}
		return nil
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	totalPages := (totalCount + pageSize - 1) / pageSize
	if totalPages == 0 && totalCount > 0 {
		totalPages = 1
	}

	response := MappingsResponse{
		Mappings:   paginatedMappings,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleAdminMappingsPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/admin_mappings.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
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
			Name:     cookieName,
			Value:    sessionToken,
			Expires:  time.Now().Add(sessionExpiry),
			Path:     "/",
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
		Name:     cookieName,
		Value:    "",
		Expires:  time.Now().AddDate(-1, 0, 0), // Expire immediately
		Path:     "/",
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
		Message         string // Added for mock generation feedback
	}{
		IsAuthenticated: isAuthenticated,
		Error:           r.URL.Query().Get("error") == "1",
		Message:         r.URL.Query().Get("message"),
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

	// Store URLData struct
	data := URLData{
		OriginalURL: originalURL,
		Count:       0,
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(shortURL), dataBytes)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"short_url": fmt.Sprintf("%s/s/%s", baseURL, shortURL)})
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	shortURL := r.URL.Path[len("/s/"):]
	if shortURL == "" {
		http.NotFound(w, r)
		return
	}

	var urlData URLData
	err := db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(shortURL))
		if err != nil {
			return err
		}
		valCopy, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		// Try to unmarshal as URLData
		jsonErr := json.Unmarshal(valCopy, &urlData)
		if jsonErr != nil { // If unmarshaling fails, assume it's an old plain string
			urlData = URLData{
				OriginalURL: string(valCopy),
				Count:       0,
			}
		}

		// Parse User-Agent
		ua := user_agent.New(r.UserAgent())

		osName := ua.OS()
		browserName, _ := ua.Browser()

		urlData.OS = osName
		urlData.Device = browserName // Using browser as a proxy for device for now

		urlData.Count++
		updatedDataBytes, err := json.Marshal(urlData)
		if err != nil {
			return err
		}
		return txn.Set([]byte(shortURL), updatedDataBytes)
	})

	if err != nil {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, urlData.OriginalURL, http.StatusFound)
}

func handleMockShorten(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	countStr := r.FormValue("count")
	count, err := strconv.Atoi(countStr)
	if err != nil || count <= 0 {
		http.Error(w, "Invalid count parameter", http.StatusBadRequest)
		return
	}

	var generated int
	for i := 0; i < count; i++ {
		originalURL := fmt.Sprintf("http://example.com/long/url/%d/%d", time.Now().UnixNano(), i)
		shortURL := generateShortURL(originalURL)

		data := URLData{
			OriginalURL: originalURL,
			Count:       0,
		}
		dataBytes, err := json.Marshal(data)
		if err != nil {
			log.Printf("Error marshaling URLData: %v", err)
			continue
		}

		err = db.Update(func(txn *badger.Txn) error {
			return txn.Set([]byte(shortURL), dataBytes)
		})
		if err != nil {
			log.Printf("Error setting short URL in DB: %v", err)
			continue
		}
		generated++
	}

	http.Redirect(w, r, fmt.Sprintf("/admin?message=Successfully generated %d mock short URLs.", generated), http.StatusSeeOther)
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

// generatePageNumbers generates a slice of page numbers to display in pagination controls.
func generatePageNumbers(currentPage, totalPages int) []int {
	var pageNumbers []int
	startPage := currentPage - 2
	if startPage < 1 {
		startPage = 1
	}
	endPage := currentPage + 2
	if endPage > totalPages {
		endPage = totalPages
	}

	for i := startPage; i <= endPage; i++ {
		pageNumbers = append(pageNumbers, i)
	}
	return pageNumbers
}
