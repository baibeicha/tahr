# Interactive REST Client (`rest-client`)

In-editor HTTP and REST client for testing backend endpoints and APIs in Tahr IDE.

## Features
- **File Format**: Write requests in `.http` or `.rest` files using standard syntax:
  ```http
  ### Get Users List
  GET https://api.example.com/v1/users
  Authorization: Bearer {{token}}
  Accept: application/json

  ### Create New Post
  POST https://api.example.com/v1/posts
  Content-Type: application/json

  {
    "title": "Tahr IDE",
    "body": "Zero-CGO High Performance Architecture"
  }
  ```
- **CodeLens & Shortcuts**: Press `Ctrl+Enter` on any request line or click the CodeLens `▶ Send Request` above it.
- **Split Response View**: Renders status code, roundtrip duration (e.g. `200 OK (28 ms)`), response headers, and formatted JSON/XML body in an adjacent split pane.
- **Environments**: Variable interpolation `{{variable}}` from `http-client.env.json` and `.env` files.
