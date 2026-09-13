# Grocerly

User-friendly grocery management system with a Next.js cashier UI and a Go REST API.

## Structure

- `apps/web` — Next.js 15 frontend (dashboard and fast POS)
- `services/api` — Go API for products, sales, procurement, finance and accounting
- `database/migrations` — ordered SQL migrations

## Run with Docker (recommended)

```powershell
docker compose up -d --build
```

The web app is on `http://localhost:3001`, the API on `http://localhost:8080`.

After adding a new migration file, apply it to the running database:

```powershell
sh database/migrate.sh
```

## Run locally without Docker

Install Node.js and Go first. Then, in separate terminals:

```powershell
cd apps/web; npm install; npm run dev
cd services/api; go run .
```

The web app is available at `http://localhost:3000`; the API uses port `8080`.
