# deckbuilder

[![CI](https://github.com/aplunk25/deckbuilder/actions/workflows/ci.yml/badge.svg)](https://github.com/aplunk25/deckbuilder/actions/workflows/ci.yml)
[![Deploy](https://github.com/aplunk25/deckbuilder/actions/workflows/deploy.yml/badge.svg)](https://github.com/aplunk25/deckbuilder/actions/workflows/deploy.yml)

A Riftbound deckbuilder written in Go: a command-line tool plus a serverless
API on AWS Lambda. Card data comes from the [Riftcodex API](https://riftcodex.com).

```
                 ┌──────────────────── AWS ─────────────────────┐
 EventBridge ───▶│ sync Lambda ──▶ Riftcodex API                │
 (daily cron)    │      │ cards.json                            │
                 │      ▼                                       │
                 │  S3 bucket                                   │
                 │      ▲ read + cached in memory               │
  CLI / curl ───▶│ API Gateway (HTTP) ──▶ api Lambda            │
                 └──────────────────────────────────────────────┘
```

## CLI

```sh
make build
./deckbuilder sync                                  # cache all cards locally
./deckbuilder search -type legend -tag jinx
./deckbuilder deck new jinx "Loose Cannon"
./deckbuilder deck champion jinx "Jinx - Rebel"
./deckbuilder deck add jinx "Blast Corps Cadet" 3
./deckbuilder deck add jinx "Fury Rune" 6           # runes/battlefields are routed automatically
./deckbuilder deck show jinx                        # decklist + rule check
./deckbuilder deck check jinx                       # exits 1 if the deck is illegal
./deckbuilder deck check jinx --remote              # validate with the deployed API
```

Run `./deckbuilder help` for every command. Cards and decks are stored in
`$RIFTBOUND_HOME` (default `~/.riftbound`). `--remote` uses `$RIFTBOUND_API_URL`.

Also available as a container:

```sh
make docker-build
make docker-run ARGS="search -type champion -domain fury"
```

## API

| Method | Path              | Description                                                        |
|--------|-------------------|--------------------------------------------------------------------|
| GET    | `/health`         | Card count and when card data was last synced                      |
| GET    | `/cards`          | Search: `q`, `type`, `domain`, `set`, `tag`, `energy`, `all`, `limit` |
| GET    | `/cards/{ref}`    | One card by Riftbound ID or name (404 not found, 409 ambiguous)    |
| POST   | `/decks/validate` | Validate a deck (same JSON the CLI saves); returns `legal` and `problems` |

```sh
curl "$RIFTBOUND_API_URL/cards?type=legend&domain=fury"
curl -X POST --data @~/.riftbound/decks/jinx.json "$RIFTBOUND_API_URL/decks/validate"
```

Run it locally against your CLI card cache with `make api-local` (serves on `:8080`).

## Deck rules checked

- 1 legend; its domains define which cards are allowed (Colorless is always allowed)
- 1 chosen champion unit that shares the legend's champion tag
- Main deck of at least 40 cards (including the chosen champion)
- At most 3 copies of a card by name; alternate printings count together
- At most 3 signature cards, all matching the legend's champion
- Exactly 12 runes and 3 different battlefields; no tokens

## CI/CD

| Workflow | Trigger | What it does |
|----------|---------|--------------|
| [ci.yml](.github/workflows/ci.yml) | push to main, PRs | gofmt, vet, golangci-lint, race tests + coverage (Linux & macOS), CLI and Lambda builds, Terraform fmt/validate, Docker build + smoke test |
| [deploy.yml](.github/workflows/deploy.yml) | push to main touching the API or infra | tests, builds Lambdas, `terraform plan/apply` via OIDC, seeds card data on first deploy, health-check smoke test |
| [release.yml](.github/workflows/release.yml) | `v*` tag | GoReleaser binaries for Linux/macOS/Windows, multi-arch image to `ghcr.io` |

Dependabot keeps Go modules, Actions and the Docker base image up to date.
Run `make ci` to run the CI checks locally.

## Deploying to AWS

Everything is defined in Terraform under [infra/](infra). GitHub Actions
authenticates with OIDC, so no AWS access keys are stored in GitHub.

1. **Bootstrap once** with your own AWS credentials. This creates the Terraform state
   bucket and the GitHub deploy role:
   ```sh
   cd infra/bootstrap
   terraform init && terraform apply
   ```
2. **Configure GitHub**: create an environment named `production` (Settings →
   Environments) and add two environment variables from the bootstrap outputs:
   - `AWS_DEPLOY_ROLE_ARN` = `deploy_role_arn`
   - `TF_STATE_BUCKET` = `state_bucket`
3. **Deploy** by pushing to `main` or running the Deploy workflow manually. The API
   URL appears on the workflow run and in `terraform output api_url`.

Deploying by hand works too:

```sh
make lambda
cd infra
terraform init -backend-config="bucket=<state_bucket>"
terraform apply
aws lambda invoke --function-name deckbuilder-sync /dev/stdout   # first-time card load
```

Cost: Lambda, API Gateway, S3 and EventBridge at this traffic fit within the
AWS free tier. API Gateway throttling (10 req/s, burst 20) limits runaway usage.

## Layout

```
src.go                  CLI
cmd/api-lambda          HTTP API (Lambda via API Gateway, or a local server)
cmd/sync-lambda         scheduled job: Riftcodex → S3
internal/riftcodex      Riftcodex API client
internal/cards          card database, lookup and search
internal/cardstore      S3-backed card storage with in-memory caching
internal/deck           deck model, storage and validation
internal/api            HTTP handlers (net/http, tested with httptest)
infra/                  Terraform for the AWS stack
infra/bootstrap         one-time Terraform: state bucket + GitHub OIDC role
```
