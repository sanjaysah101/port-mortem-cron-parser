.PHONY: all setup baseline probes diff clean

all: setup baseline probes diff

## Clone + build upstream at the pinned commit, install deps.
setup:
	@test -d upstream || git clone https://github.com/harrisiirak/cron-parser.git upstream
	cd upstream && git checkout --quiet 8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4
	cd upstream && npm ci --silent && npm run build --silent
	cd goprobe && go mod tidy

## Upstream suite, unmodified. Requires TZ=UTC (see DECISIONS.md D7).
baseline:
	cd upstream && TZ=UTC npx jest --silent
	@echo "--- upstream tree must show only untracked _probe/ ---"
	cd upstream && git status --porcelain

## Reproductions for each finding.
probes:
	cd upstream && node _probe/tzfacts.mjs
	cd upstream && node _probe/repro2.mjs
	cd upstream && node _probe/verify1b.mjs
	cd upstream && node _probe/pr435.mjs
	cd upstream && node _probe/fallback.mjs
	cd goprobe && go run .
	cd goprobe && go run -tags offsets offsets.go

## Differential: both emitters over the shared corpus, then diff.
diff:
	cd corpus && node emit_ts.mjs > out_ts.json
	cd goprobe && go run -tags emit emit_go.go > ../corpus/out_go.json
	cd corpus && node diff.mjs out_ts.json out_go.json

clean:
	rm -f corpus/out_ts.json corpus/out_go.json corpus/err_*.txt
