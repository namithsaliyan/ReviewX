package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "sync"

    _ "github.com/mattn/go-sqlite3"
)

// Review represents a review submitted by a user
type Review struct {
    ID     int    `json:"id"`
    Name   string `json:"name"`
    Review string `json:"review"`
    Rating int    `json:"rating"` // New field to store the rating
}

// Database connection
var db *sql.DB
var mutex = &sync.Mutex{}
var idCounter = 0

func main() {
    var err error
    // Open SQLite database
    db, err = sql.Open("sqlite3", "./reviews.db")
    if err != nil {
        log.Fatalf("Failed to connect to database: %v", err)
    }
    defer db.Close()

    // Initialize the database schema
    if err := initializeDatabase(); err != nil {
        log.Fatalf("Failed to initialize database: %v", err)
    }

    // Load the highest ID from the database
    loadIDCounter()

    http.HandleFunc("/reviews", withCORS(reviewsHandler))
    http.HandleFunc("/delete-review", withCORS(deleteReviewHandler)) // Handler for deleting a review

    fmt.Println("Server is listening on port 8080...")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

// withCORS is a middleware function that adds CORS headers
func withCORS(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
        
        // Handle preflight OPTIONS request
        if r.Method == http.MethodOptions {
            return
        }
        
        next(w, r)
    }
}

// initializeDatabase creates the reviews table if it does not exist
func initializeDatabase() error {
    schema := `
    CREATE TABLE IF NOT EXISTS reviews (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT,
        review TEXT,
        rating INTEGER
    );
    `
    _, err := db.Exec(schema)
    return err
}

// loadIDCounter retrieves the highest ID from the database to set the counter
func loadIDCounter() {
    row := db.QueryRow("SELECT MAX(id) FROM reviews")
    var maxID sql.NullInt64
    if err := row.Scan(&maxID); err != nil {
        log.Fatalf("Failed to load highest review ID: %v", err)
    }
    if maxID.Valid {
        idCounter = int(maxID.Int64)
    }
}

// saveReview inserts a new review into the database
func saveReview(review *Review) error {
    _, err := db.Exec("INSERT INTO reviews (name, review, rating) VALUES (?, ?, ?)", review.Name, review.Review, review.Rating)
    return err
}

// deleteReview removes a review by ID from the database and returns an error if no review is found
func deleteReview(id int) error {
    result, err := db.Exec("DELETE FROM reviews WHERE id = ?", id)
    if err != nil {
        return err
    }

    // Check how many rows were affected
    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return err
    }

    if rowsAffected == 0 {
        return fmt.Errorf("no review found with id %d", id)
    }

    return nil
}

// loadReviews retrieves all reviews from the database
func loadReviews() ([]Review, error) {
    rows, err := db.Query("SELECT id, name, review, rating FROM reviews")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var reviews []Review
    for rows.Next() {
        var review Review
        if err := rows.Scan(&review.ID, &review.Name, &review.Review, &review.Rating); err != nil {
            return nil, err
        }
        reviews = append(reviews, review)
    }
    return reviews, nil
}

// reviewsHandler handles both POST and GET requests for reviews
func reviewsHandler(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodPost:
        handlePostReview(w, r)
    case http.MethodGet:
        handleGetReviews(w, r)
    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}

// handlePostReview handles the submission of a new review
func handlePostReview(w http.ResponseWriter, r *http.Request) {
    // Parse the JSON request body
    var newReview Review
    if err := json.NewDecoder(r.Body).Decode(&newReview); err != nil {
        http.Error(w, "Invalid request payload", http.StatusBadRequest)
        return
    }

    // Validate the rating value
    if newReview.Rating < 1 || newReview.Rating > 5 {
        http.Error(w, "Invalid rating value. Must be between 1 and 5.", http.StatusBadRequest)
        return
    }

    // Lock the mutex before modifying the database
    mutex.Lock()
    defer mutex.Unlock()

    // Assign a unique ID to the new review
    idCounter++
    newReview.ID = idCounter

    // Save the review to the database
    if err := saveReview(&newReview); err != nil {
        http.Error(w, "Failed to save review", http.StatusInternalServerError)
        return
    }

    // Respond with success and the assigned ID
    response := map[string]interface{}{"success": true, "id": newReview.ID}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// handleGetReviews handles fetching all submitted reviews
func handleGetReviews(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")

    // Lock the mutex before reading the database
    mutex.Lock()
    defer mutex.Unlock()

    reviews, err := loadReviews()
    if err != nil {
        http.Error(w, "Failed to load reviews", http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(reviews)
}

// deleteReviewHandler handles the deletion of a review by ID
func deleteReviewHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        respondWithJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
        return
    }

    // Parse the JSON request body to get the ID of the review to delete
    var requestData struct {
        ID int `json:"id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
        respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
        return
    }

    // Lock the mutex before modifying the database
    mutex.Lock()
    defer mutex.Unlock()

    // Remove the review from the database
    if err := deleteReview(requestData.ID); err != nil {
        respondWithJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to delete review: %v", err)})
        return
    }

    // Respond with success
    respondWithJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// respondWithJSON writes a JSON response to the ResponseWriter
func respondWithJSON(w http.ResponseWriter, status int, payload interface{}) {
    response, err := json.Marshal(payload)
    if err != nil {
        http.Error(w, "Failed to marshal JSON response", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    w.Write(response)
}
