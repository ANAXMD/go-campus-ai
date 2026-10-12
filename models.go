package main

type Student struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Major string `json:"major"`
}

type Course struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Credits   int    `json:"credits"`
	Professor string `json:"professor"`
}

type User struct {
	ID           int    `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"` // The "-" prevents the password hash from ever being sent in a JSON response
	CreatedAt    string `json:"created_at"`
}
