package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/rs/cors"
)

// The Unified Switchboard
func studentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		// 1. Try to fetch from Redis first
		cachedStudents, err := Cache.Get(Ctx, "all_students").Result()
		if err == nil {
			// CACHE HIT: Data exists in RAM. Send it immediately!
			log.Println("[REDIS] Cache Hit! Serving from RAM ⚡")
			w.Write([]byte(cachedStudents))
			return
		}

		// CACHE MISS: Data is not in Redis. Ask PostgreSQL.
		log.Println("[POSTGRES] Cache Miss! Querying hard drive 🐢")
		rows, err := db.Query("SELECT id, name, email, major FROM students")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var students []Student
		for rows.Next() {
			var s Student
			if err := rows.Scan(&s.ID, &s.Name, &s.Email, &s.Major); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			students = append(students, s)
		}

		// 2. Convert the Postgres data to JSON
		studentsJSON, _ := json.Marshal(students)

		// 3. Save the JSON in Redis for 10 minutes so the next request is blazing fast
		Cache.Set(Ctx, "all_students", studentsJSON, 10*time.Minute)

		w.Write(studentsJSON)

	case "POST":
		var s Student
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		sqlStatement := `INSERT INTO students (name, email, major) VALUES ($1, $2, $3) RETURNING id`
		err := db.QueryRow(sqlStatement, s.Name, s.Email, s.Major).Scan(&s.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		go sendWelcomeEmail(s.Name, s.Email)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(s)

	case "PUT":
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing student ID in URL", http.StatusBadRequest)
			return
		}

		var s Student
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		sqlStatement := `UPDATE students SET name=$1, email=$2, major=$3 WHERE id=$4`
		_, err := db.Exec(sqlStatement, s.Name, s.Email, s.Major, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Student updated successfully"}`))

	case "DELETE":
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing student ID in URL", http.StatusBadRequest)
			return
		}

		sqlStatement := `DELETE FROM students WHERE id=$1`
		_, err := db.Exec(sqlStatement, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Student deleted successfully"}`))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "API is alive"}`))
}

func sendWelcomeEmail(name string, email string) {
	log.Printf("[EMAIL THREAD] Preparing to send welcome email to %s (%s)...\n", name, email)

	// Simulate the time it takes to talk to an external email server (like SendGrid or AWS SES)
	time.Sleep(3 * time.Second)

	log.Printf("[EMAIL THREAD] SUCCESS: Email delivered to %s!\n", email)
}

func main() {
	initDB()    // From db.go
	initCache() // From cache.go
	defer db.Close()

	http.HandleFunc("/api/health", healthHandler)

	// Lock the route with JWT!
	http.HandleFunc("/api/students", requestLogger(RequireJWT(studentsHandler)))

	// ADDED REGISTRATION ROUTE HERE
	http.HandleFunc("/api/register", RegisterUser)
	// ADDED ROUTES HERE
	http.HandleFunc("/api/login", LoginUser)
	// ADD THIS CORS CONFIGURATION:
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // Allows any frontend to connect (useful for local dev)
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
	})

	log.Println("REST API live on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", c.Handler(http.DefaultServeMux)))
}
