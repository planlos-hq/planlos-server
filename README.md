# planlos-server

Go-Backend (REST-API) des Stundenplan-Assistenzsystems PlanLos.

## Starten mit Docker Compose

Die Datenbank läuft separat aus [planlos-db](https://github.com/planlos-hq/planlos-db),
diese muss zuerst laufen. Beide Stacks verbinden sich über das Netzwerk
`planlos-network`, das planlos-db anlegt.

```bash
cp .env.example .env   # gleiches Passwort wie in planlos-db/.env
docker compose up -d --build   # API auf http://127.0.0.1:8004
```

Der Server migriert das Datenbankschema beim Start selbst. Backups liegen im
Volume `backups`.

| Variable            | Bedeutung                                                |
| ------------------- | -------------------------------------------------------- |
| `POSTGRES_PASSWORD` | Datenbankpasswort, wie in planlos-db (Pflicht)           |
| `DB_HOST`           | Datenbank im gemeinsamen Netzwerk (Standard `planlos-db`) |
| `PLANLOS_NETWORK`   | Netzwerk von planlos-db (Standard `planlos-network`)     |
| `NO_AUTH`           | Nicht leer = keine Nutzerprüfung (nur zur Entwicklung!)  |
| `SERVER_PORT`       | Port auf dem Host (Standard `8004`)                      |

## Entwicklung

Mit Nix: `direnv allow` bzw. `nix develop`.

```bash
DATABASE_URL=postgres://planlos:<passwort>@localhost:8003/planlos go run .
go test ./...
./format_sql.sh   # SQL formatieren (pgFormatter 5.6 wie in CI)
```

Tests und Formatprüfung laufen in CI über das `test`-Target im
[`Dockerfile`](Dockerfile): `docker build --target test .`

## Datenbankmigrationen

`db/` ist das Git-Submodul [planlos-db](https://github.com/planlos-hq/planlos-db).
Unter `db/migrations/<version>/` liegen je ein vollständiges `schema.sql` und
ein `up.sql` für die Migration von der Vorversion. Die Migrationen werden in
das Binary eingebettet.

```bash
git clone --recurse-submodules git@github.com:planlos-hq/planlos-server.git
git submodule update --init   # in einem bestehenden Klon
git submodule update --remote db   # auf neueste Migrationen aktualisieren
```

**Keine Passwörter, Schlüssel, echten Namen, Benutzernamen, Hostnamen oder
Serverkonfigurationen committen.**
