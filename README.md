# MCP Crawler Service

This project consists of a Go backend server and a React frontend for managing and running MCP servers and tools.

---

## Getting Started

### 1. Start the Backend Server

From the project root directory, run:

```bash
go run main.go
```

The backend server will start, typically on port `8080` (unless overridden by environment variables).

---

### 2. Start the Frontend

From the project root, run:

```bash
cd frontend/
npm install
npm run dev
```

The frontend will start, typically on port `5175` (Vite default).

---

## Environment Variables

The backend server requires the following environment variables:

| Key            | Description                                   | Example Value                       |
| -------------- | --------------------------------------------- | ----------------------------------- |
| `DATABASE_URL` | PostgreSQL connection string                  | `postgres://user:pass@localhost/db` |
| `PORT`         | Port for the backend server (default: `8080`) | `8080`                              |
| OPENAI_API_KEY | OpenAI API key                                | `sk-...`                            |
| GITHUB_TOKEN   | GitHub token                                  | `ghp_...`                           |

**Set these in your shell or a `.env` file before running the backend.**

The frontend may require the following environment variables (set in `frontend/.env`):

| Key            | Description               | Example Value           |
| -------------- | ------------------------- | ----------------------- |
| `VITE_API_URL` | URL of the backend server | `http://localhost:8080` |

---

## Example Usage

1. Start the backend:

   ```bash
   DATABASE_URL=postgres://user:pass@localhost/db go run main.go
   ```

2. Start the frontend:

   ```bash
   cd frontend
   npm install
   npm run dev
   ```

3. Open your browser to [http://localhost:5175](http://localhost:5175) to use the UI.

---

## Notes

- Ensure PostgreSQL is running and accessible via `DATABASE_URL`.
- The backend and frontend servers can run concurrently.
- For development, CORS is enabled on the backend.

---
