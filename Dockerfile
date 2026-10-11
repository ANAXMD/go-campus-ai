# 1. Base Image: We start with a microscopic version of Linux that has Go pre-installed
FROM golang:1.26.7-alpine

# 2. Working Directory: Create a folder inside the virtual machine to hold our code
WORKDIR /app

# 3. Dependencies: Copy your go.mod and go.sum files first, and download the Postgres driver
COPY go.mod go.sum ./
RUN go mod download

# 4. Source Code: Copy the rest of your Go files, middleware, and Makefile into the container
COPY . .

# 5. Compilation: Build the binary INSIDE the virtual machine
RUN go build -o bin/api .

# 6. Networking: Tell the container to expose port 8080 to the outside world
EXPOSE 8080

# 7. Boot Command: When the container starts, execute the compiled binary
CMD ["./bin/api"] 