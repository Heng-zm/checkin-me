# Render build fix: missing go.sum entries

If Render shows errors like:

```text
missing go.sum entry for module providing package github.com/go-chi/chi/v5
```

The project was pushed without `go.sum`. The Dockerfile has been updated to build with:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -o /checkinme-api ./cmd/api
```

This allows Go to resolve and write missing module checksums inside the Docker build layer.

Recommended permanent fix on your local computer:

```bash
go mod tidy
git add go.mod go.sum Dockerfile
git commit -m "Fix Render Go module checksums"
git push
```

Do not run the suggested `go get github.com/hengk7401/checkinme-go-api/internal/...` commands. Those are just Go's generic error hints and are not the correct fix for this project.
