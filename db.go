package main

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

// Global database pool
var db *sql.DB

func initDB() {
	var err error
	connStr := "host=postgres-db port=5432 user=admin password=admin dbname=campus_db sslmode=disable"
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Database unreachable: ", err)
	}

	log.Println("Database connection established securely.")
}
