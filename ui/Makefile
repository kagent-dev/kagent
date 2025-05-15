

.PHONY: build
build:
	@echo "Updating UI lock file..."
	npm install --frozen-lockfile --production
	npm update  --lockfile-only
	@echo "Updating UI lock file done."