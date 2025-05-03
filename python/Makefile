export GIT_LFS_SKIP_SMUDGE=1

### Autogen Studio Patch management

.PHONY: clean
clean:
	rm -rf tmp/autogen
	rm -rf tmp/autogenstudio

.PHONY: studio-checkout
studio-checkout: clean
	mkdir -p tmp
	git clone git@github.com:microsoft/autogen.git tmp/autogen
	mv tmp/autogen/python/packages/autogen-studio/autogenstudio tmp/

.PHONY: studio-compare
studio-compare: studio-checkout
	git diff --no-index --color=never src/autogenstudio tmp/autogenstudio | tee patches/autogenstudio.diff

studio-apply: studio-checkout
	rm -rf src/autogenstudio
	cp -r tmp/autogenstudio src/
	git apply patches/autogenstudio.diff

### Build and Test

.PHONY: update
update:
	uv sync --all-extras --all-groups

.PHONY: format
format:
	uv run ruff check

.PHONY: test
test: update
	uv run pytest tests

.PHONY: dependency-tree
dependency-tree: update
	uv tree --all-groups > dependency-tree.txt

.PHONY: build
build: clean update format dependency-tree
