# URL Shortener

This is a simple URL shortening service built with Go and BadgerDB.

## Features

- Shorten long URLs into concise, easy-to-share links.
- Redirect shortened URLs to their original destinations.
- Uses BadgerDB for persistent storage of URL mappings.

## Technologies Used

- **Go**: The primary programming language.
- **BadgerDB**: An embeddable, persistent, and fast key-value store.
- **Docker**: For containerization and easy deployment.

## Getting Started

### Prerequisites

- Go (version 1.23 or higher)
- Docker (if you plan to use Docker)

### Build and Run Locally

1.  **Clone the repository:**

    ```bash
    git clone <repository_url>
    cd url-shortener
    ```

2.  **Install dependencies:**

    ```bash
    go mod tidy
    ```

3.  **Run the application:**

    ```bash
    go run main.go
    ```

    The server will start on `http://localhost:8080`.

### Build and Run with Docker

1.  **Build the Docker image:**

    ```bash
    docker build -t url-shortener .
    ```

2.  **Run the Docker container:**

    ```bash
    docker run -d -p 8080:8080 url-shortener
    ```

    The application will be accessible at `http://localhost:8080`.

## Usage

### Shorten a URL

Send a POST request to `/shorten` with the `url` parameter:

```bash
curl -X POST -d "url=https://www.example.com" http://localhost:8080/shorten
```

This will return a shortened URL like `http://localhost:8080/s/yourshortcode`.

### Redirect to Original URL

Access the shortened URL in your browser or with `curl`:

```bash
curl -L http://localhost:8080/s/yourshortcode
```

This will redirect you to the original `https://www.example.com`.
