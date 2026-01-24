package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	_ "github.com/lib/pq"
)

type CallRequest struct {
	ClientCampaignModelID int     `json:"client_campaign_model_id"`
	Number                string  `json:"number"`
	Transcription         *string `json:"transcription"`
	Stage                 *int    `json:"stage"`
	VoiceName             *string `json:"voice_name"`
	ResponseCategoryName  *string `json:"response_category_name"`
	ListID                *string `json:"list_id"`
	Transferred           bool    `json:"transferred"`
	DispoPunched          *bool   `json:"dispo_punched"`
}

type CallResponse struct {
	ID        int       `json:"id"`
	Number    string    `json:"number"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// Cache structures
type Cache struct {
	mu                 sync.RWMutex
	responseCategories map[string]int
	voices             map[string]int
	campaigns          map[int]bool
}

var (
	db    *sql.DB
	cache *Cache
)

func init() {
	cache = &Cache{
		responseCategories: make(map[string]int),
		voices:             make(map[string]int),
		campaigns:          make(map[int]bool),
	}
}

func main() {
	godotenv.Load()
	// Database connection from environment variables
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	// Validate required environment variables
	if dbHost == "" || dbPort == "" || dbUser == "" || dbPass == "" || dbName == "" {
		log.Fatal("Missing required database environment variables: DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME")
	}

	connStr := "host=" + dbHost + " port=" + dbPort + " user=" + dbUser +
		" password=" + dbPass + " dbname=" + dbName + " sslmode=disable"

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Configure connection pool
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	log.Println("Database connected successfully")

	// Preload caches
	preloadCaches()

	// Echo setup
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Routes
	e.POST("/api/calls", createCall)
	e.GET("/health", health)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s", port)
	e.Logger.Fatal(e.Start(":" + port))
}

func createCall(c echo.Context) error {
	req := new(CallRequest)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	// Validate required fields
	if req.Number == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Number is required"})
	}

	// Verify campaign exists (with cache)
	if !campaignExists(req.ClientCampaignModelID) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid client_campaign_model_id"})
	}

	// Resolve foreign keys
	var voiceID *int
	if req.VoiceName != nil && *req.VoiceName != "" {
		id, err := getVoiceID(*req.VoiceName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Voice doesn't exist"})
		}
		voiceID = &id
	}

	var responseCategoryID *int
	if req.ResponseCategoryName != nil && *req.ResponseCategoryName != "" {
		id, err := getResponseCategoryID(*req.ResponseCategoryName)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Response category doesn't exist"})
		}
		responseCategoryID = &id
	}

	// Insert call
	query := `
		INSERT INTO calls (
			client_campaign_model_id, number, transcription, stage, 
			voice_id, response_category_id, list_id, transferred, dispo_punched, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, timestamp
	`

	var callID int
	var timestamp time.Time

	err := db.QueryRow(
		query,
		req.ClientCampaignModelID,
		req.Number,
		req.Transcription,
		req.Stage,
		voiceID,
		responseCategoryID,
		req.ListID,
		req.Transferred,
		req.DispoPunched,
		time.Now(),
	).Scan(&callID, &timestamp)

	if err != nil {
		log.Println("Error inserting call:", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save call"})
	}

	response := CallResponse{
		ID:        callID,
		Number:    req.Number,
		Timestamp: timestamp,
		Message:   "Call saved successfully",
	}

	return c.JSON(http.StatusCreated, response)
}

func campaignExists(id int) bool {
	// Check cache first
	cache.mu.RLock()
	exists, found := cache.campaigns[id]
	cache.mu.RUnlock()

	if found {
		return exists
	}

	// Check database
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM client_campaign_model WHERE id = $1", id).Scan(&count)
	if err != nil {
		log.Println("Error checking campaign:", err)
		return false
	}

	exists = count > 0

	// Update cache
	cache.mu.Lock()
	cache.campaigns[id] = exists
	cache.mu.Unlock()

	return exists
}

func getResponseCategoryID(name string) (int, error) {
	// Check cache first
	cache.mu.RLock()
	id, found := cache.responseCategories[name]
	cache.mu.RUnlock()

	if found {
		return id, nil
	}

	// Check database
	err := db.QueryRow("SELECT id FROM response_categories WHERE name = $1", name).Scan(&id)
	if err != nil {
		log.Printf("Response category not found: %s", name)
		return 0, err
	}

	// Update cache
	cache.mu.Lock()
	cache.responseCategories[name] = id
	cache.mu.Unlock()

	return id, nil
}

func getVoiceID(name string) (int, error) {
	// Check cache first
	cache.mu.RLock()
	id, found := cache.voices[name]
	cache.mu.RUnlock()

	if found {
		return id, nil
	}

	// Check database
	err := db.QueryRow("SELECT id FROM voices WHERE name = $1", name).Scan(&id)
	if err != nil {
		log.Printf("Voice not found: %s", name)
		return 0, err
	}

	// Update cache
	cache.mu.Lock()
	cache.voices[name] = id
	cache.mu.Unlock()

	return id, nil
}

func preloadCaches() {
	log.Println("Preloading caches...")

	// Preload response categories
	rows, err := db.Query("SELECT id, name FROM response_categories")
	if err != nil {
		log.Println("Error preloading response categories:", err)
	} else {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err == nil {
				cache.responseCategories[name] = id
				count++
			}
		}
		log.Printf("Preloaded %d response categories", count)
	}

	// Preload voices
	rows, err = db.Query("SELECT id, name FROM voices")
	if err != nil {
		log.Println("Error preloading voices:", err)
	} else {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err == nil {
				cache.voices[name] = id
				count++
			}
		}
		log.Printf("Preloaded %d voices", count)
	}

	// Preload active campaigns
	rows, err = db.Query("SELECT id FROM client_campaign_model WHERE is_enabled = true")
	if err != nil {
		log.Println("Error preloading campaigns:", err)
	} else {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err == nil {
				cache.campaigns[id] = true
				count++
			}
		}
		log.Printf("Preloaded %d campaigns", count)
	}
}

func health(c echo.Context) error {
	if err := db.Ping(); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "healthy"})
}
