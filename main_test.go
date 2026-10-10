package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// In Go, every test function must start with "Test" and take a pointer to testing.T
func TestHealthCheck(t *testing.T) {
	// 1. Create a fake HTTP request to our new route
	req, err := http.NewRequest("GET", "/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create a fake browser (ResponseRecorder) to capture the server's reply
	rr := httptest.NewRecorder()

	// 3. Fire the fake request directly into our handler function
	healthHandler(rr, req)

	// 4. Assert the result: Did we get a 200 OK?
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// 5. Assert the result: Did it return the exact string we expect?
	expected := `{"status": "API is alive"}`
	if rr.Body.String() != expected {
		t.Errorf("Handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}
