package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

// requestLogger acts as a wrapper around any standard HTTP handler
func requestLogger(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. The Intercept: Start the stopwatch as soon as the request hits the server
		start := time.Now()

		// 2. The Pass-Through: Hand the request off to your actual database handler (e.g., getStudentsHandler)
		next(w, r)

		// 3. The Exit: Stop the stopwatch after the database finishes
		duration := time.Since(start)

		// 4. Print the diagnostic log to the terminal
		log.Printf("Intercepted: [%s] %s | Execution Time: %v\n", r.Method, r.URL.Path, duration)
	}
}

// requireAPIKey blocks any request that doesn't have the correct secret header
func requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")

		// Pull the expected secret directly from the Linux OS
		expectedKey := os.Getenv("CAMPUS_SECRET")

		if key != expectedKey || expectedKey == "" {
			log.Printf("BLOCKED: Unauthorized access attempt from %s\n", r.RemoteAddr)
			http.Error(w, "Access Denied: Invalid API Key", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
