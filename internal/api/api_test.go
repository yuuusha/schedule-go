package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"schedule/internal/config"
	"schedule/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConcurrentBookingConflict(t *testing.T) {

	dbURL := "postgres://schedule:local_dev_only@localhost:5432/schedule?sslmode=disable"
	ctx := context.Background()

	db, e := pgxpool.New(ctx, dbURL)
	if e != nil {
		t.Skip("Skipping test: PostgreSQL not available. Run docker compose up first")
		return
	}
	defer db.Close()

	cfg := config.Config{
		Addr:        ":0",
		DSN:         dbURL,
		Migrations:  "../../migrations",
		MinDuration: 15 * time.Minute,
		MaxDuration: 8 * time.Hour,
		Retries:     3,
		Shutdown:    10 * time.Second,
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if e := store.Migrate(ctx, db, cfg.Migrations); e != nil {
		t.Fatalf("Migration failed: %v", e)
	}

	handler := New(db, cfg, log)
	server := httptest.NewServer(handler)
	defer server.Close()

	resourceBody := `{"name":"Test Room","type":"room","capacity":5}`
	resp, e := http.Post(server.URL+"/resources", "application/json", bytes.NewBufferString(resourceBody))
	if e != nil {
		t.Fatalf("Failed to create resource: %v", e)
	}
	defer resp.Body.Close()

	var resource map[string]any
	json.NewDecoder(resp.Body).Decode(&resource)
	resourceID := int64(resource["id"].(float64))

	user1Body := `{"name":"User1"}`
	resp, e = http.Post(server.URL+"/users", "application/json", bytes.NewBufferString(user1Body))
	if e != nil {
		t.Fatalf("Failed to create user1: %v", e)
	}
	defer resp.Body.Close()

	var user1 map[string]any
	json.NewDecoder(resp.Body).Decode(&user1)
	user1ID := int64(user1["id"].(float64))

	user2Body := `{"name":"User2"}`
	resp, e = http.Post(server.URL+"/users", "application/json", bytes.NewBufferString(user2Body))
	if e != nil {
		t.Fatalf("Failed to create user2: %v", e)
	}
	defer resp.Body.Close()

	var user2 map[string]any
	json.NewDecoder(resp.Body).Decode(&user2)
	user2ID := int64(user2["id"].(float64))

	startTime := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339Nano)
	endTime := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339Nano)

	bookingBody := func(userID int64) string {
		return fmt.Sprintf(`{"resource_id":%d,"user_id":%d,"start_at":"%s","end_at":"%s"}`, resourceID, userID, startTime, endTime)
	}

	var wg sync.WaitGroup
	results := make(chan int, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()
		resp, e := http.Post(server.URL+"/bookings", "application/json", bytes.NewBufferString(bookingBody(user1ID)))
		if e != nil {
			results <- 0
			return
		}
		defer resp.Body.Close()
		results <- resp.StatusCode
	}()

	go func() {
		defer wg.Done()
		resp, e := http.Post(server.URL+"/bookings", "application/json", bytes.NewBufferString(bookingBody(user2ID)))
		if e != nil {
			results <- 0
			return
		}
		defer resp.Body.Close()
		results <- resp.StatusCode
	}()

	wg.Wait()
	close(results)

	var statuses []int
	for status := range results {
		statuses = append(statuses, status)
	}

	successCount := 0
	conflictCount := 0

	for _, status := range statuses {
		if status == 201 {
			successCount++
		}
		if status == 409 {
			conflictCount++
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful booking (201), got %d", successCount)
	}

	if conflictCount != 1 {
		t.Errorf("Expected exactly 1 conflict (409), got %d", conflictCount)
	}

	t.Logf("Test passed: %d successful, %d conflicts", successCount, conflictCount)
}
