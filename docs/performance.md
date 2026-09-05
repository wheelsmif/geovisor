# Performance target

The release target is less than 50 ms for post-extraction work: observation
compilation, canonical TIR validation/serialization, and emission of TIR,
WebMCP, MCP, strict OpenAI, and binding companions. Browser startup, navigation,
DOM stabilization, and payload execution are intentionally excluded.

`BenchmarkCorpusCompileAndEmit` builds a deterministic 100-tool observation
corpus from the committed payload contract fixture and runs the compiler and
all emitters. Run it with:

```sh
go test ./internal/integration -run '^$' -bench '^BenchmarkCorpusCompileAndEmit$' -benchmem -count=3
```

Review the three local samples on representative release hardware. Unit tests
do not fail on a narrow wall-clock assertion because machine load and power
management would make it flaky. A sustained median above 50 ms is a release
blocker and should be investigated with the same corpus before changing the
target.
