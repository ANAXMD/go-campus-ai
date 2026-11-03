package main

// Student is both the database row and the JSON shape of the API.
type Student struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Major string `json:"major"`
}

// User is an account that may sign in to the API.
type User struct {
	ID           int
	Email        string
	PasswordHash string
}
