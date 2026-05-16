# Contracts

Этот каталог хранит межмодульные контракты monorepo.

Сейчас основной контракт между backend и frontend - OpenAPI спецификация REST API:

- `openapi/swagger.yaml`
- `openapi/swagger.json`

Источник генерации находится в `backend/docs`. После изменения Swagger annotations в backend нужно регенерировать документацию и обновить копию в `contracts/openapi`.

```bash
docker run --rm -v "$PWD:/app" -w /app/backend golang:1.25-alpine \
  go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/server/main.go -o docs

cp backend/docs/swagger.yaml contracts/openapi/swagger.yaml
cp backend/docs/swagger.json contracts/openapi/swagger.json
```

Frontend, включая будущую Angular-реализацию, должен ориентироваться на этот контракт, а не на внутренние модели backend.
