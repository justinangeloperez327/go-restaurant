.PHONY: up down api-test web-check

up:
	docker compose up --build

down:
	docker compose down

api-test:
	cd apps/api && go test ./...

web-check:
	cd apps/web && npm run lint && npm run typecheck && npm run build
