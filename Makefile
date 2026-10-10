run:
	CAMPUS_SECRET="super_secret_999" go run .

test:
	go test -v

build: test
	go build -o bin/api .