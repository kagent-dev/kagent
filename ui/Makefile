
.PHONY: build
build:
	npm ci
	npm run build

.PHONY: clean
clean:
	rm -rf node_modules
	rm -rf dist
	rm -rf .cache

.PHONY: audit
audit:
	@echo "Running security audit..."
	npm audit
	@echo "Security audit completed."

.PHONY: update
update:
	@echo "Updating UI lock file..."
	npm audit fix
	npm ci
	@echo "Updating UI lock file done."
