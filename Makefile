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
	@find internal -name '*.go' ! -name '*.gen.go' ! -name '*.pb.go' -exec goimports -l -w {} +

.PHONY: lint-new
lint-new:
	@NEW_FROM=origin/main ./scripts/lint.sh

.PHONY: lint
lint:
	@./scripts/lint.sh

.PHONY: seed-stock-mock
seed-stock-mock:
	mysql -h 127.0.0.1 -P 3307 -u root -proot < scripts/seed-stock-mock.sql
