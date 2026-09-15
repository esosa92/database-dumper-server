# CLAUDE.md

## Entorno de desarrollo

- **Go NO está instalado en el host**. Está disponible solo dentro del contenedor Docker de cada proyecto.
- Para correr comandos Go (`go get`, `go build`, `go mod tidy`, etc.) usar `task connect` para entrar al contenedor y ejecutarlos ahí.
- El contenedor se levanta con `task run`.

## Estructura

- El módulo Go vive en `src/` (`go.mod`, `main.go`, `db/`, `dumper/`, `handlers/`, `templates/`). Se monta en `/go/src` dentro del contenedor.
- `pkg/`, `sdk/` y `bin/` en la raíz son caches de Go compartidos con el contenedor. Nunca poner el `go.mod` al mismo nivel que ellos: Go los recorre como parte del módulo y `go mod tidy` / `go vet ./...` rompen.
- `dump.json` y `dumper.db` van en `src/`, que es el directorio de trabajo del server.
