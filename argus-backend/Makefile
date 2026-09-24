.DEFAULT_GOAL := test
.PHONY: test test-db cover cover-html

COVERAGE := coverage.out

TEST_PG := argus-test-postgres
TEST_PG_PORT := 55432

COVER_EXCLUDE := /docs/|/mocks/

test:
	go test ./...

test-db:
	docker run -d --rm --name $(TEST_PG) -e POSTGRES_PASSWORD=test -p 127.0.0.1:$(TEST_PG_PORT):5432 postgres:16-alpine
	@until docker exec $(TEST_PG) pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 0.5; done
	TEST_POSTGRES_URL=postgres://postgres:test@127.0.0.1:$(TEST_PG_PORT)/postgres?sslmode=disable \
		go test -count=1 ./internal/storage/; status=$$?; docker rm -f $(TEST_PG) >/dev/null; exit $$status

cover:
	go test ./... -coverprofile=$(COVERAGE).tmp
	@grep -vE '$(COVER_EXCLUDE)' $(COVERAGE).tmp > $(COVERAGE)
	@rm -f $(COVERAGE).tmp
	go tool cover -func=$(COVERAGE)

cover-html: cover
	go tool cover -html=$(COVERAGE)
