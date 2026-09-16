.DEFAULT_GOAL := test
.PHONY: test cover cover-html

COVERAGE := coverage.out

COVER_EXCLUDE := /docs/|/mocks/

test:
	go test ./...

cover:
	go test ./... -coverprofile=$(COVERAGE).tmp
	@grep -vE '$(COVER_EXCLUDE)' $(COVERAGE).tmp > $(COVERAGE)
	@rm -f $(COVERAGE).tmp
	go tool cover -func=$(COVERAGE)

cover-html: cover
	go tool cover -html=$(COVERAGE)
