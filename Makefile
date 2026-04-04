.PHONY: gen
gen: genproto genopenapi

.PHONY: genproto
genproto:
	@./scripts/genproto.sh

.PHONY: genopenapi
genopenapi:
	@./scripts/genopenapi.sh

.PHONY: fmt
fmt:
	goimports -l -w internal/

.PHONT: lint
lint:
	@./scripts/lint.sh