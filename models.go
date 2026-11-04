package main

// Student is both the database row and the JSON shape of the API.
type Student struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Major string `json:"major"`
}

// Role decides what a signed-in user may do. Viewers can read the directory;
// only admins can change it.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

func (r Role) valid() bool { return r == RoleAdmin || r == RoleViewer }

// User is an account that may sign in to the API.
type User struct {
	ID           int
	Email        string
	PasswordHash string
	Role         Role
}

// StudentQuery describes one page of the directory. Search matches the name,
// email or major, case-insensitively; an empty Search matches everyone.
type StudentQuery struct {
	Search string
	Limit  int
	Offset int
}

// StudentPage is one page of results plus the size of the whole result set,
// so the UI can render "26-50 of 130".
type StudentPage struct {
	Items  []Student `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

// StudentStats are the summary numbers on the dashboard, computed by the
// database so the client never needs the full list.
type StudentStats struct {
	Total    int    `json:"total"`
	Majors   int    `json:"majors"`
	TopMajor string `json:"topMajor"`
	TopCount int    `json:"topCount"`
}
