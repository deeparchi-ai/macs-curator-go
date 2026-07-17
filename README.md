# macs-dfsms-go

MACS §7: Tiered knowledge lifecycle and memory compression for agent sessions.

**Status:** v0.1 — 13 tests

## What

Ports z/OS DFSMS (1978) tiered storage management to agent context:
- **Four tiers**: Hot (full fidelity) → Warm (summarized) → Cold (bullet-point) → Archive (searchable)
- **Automatic migration**: Token thresholds trigger tier transitions
- **Recall**: Search archive/cold tiers by keyword
- **Promote**: Restore archived context to hot tier on demand
- **Lifecycle policies**: Configurable thresholds, TTLs, chunk limits

## Usage

```go
import "github.com/deeparchi-ai/macs-dfsms-go/pkg/dfsms"

s := dfsms.NewStore(dfsms.DefaultPolicy())
s.Add(&dfsms.ContextChunk{
    ID: "ctx-1", Content: "Architecture decision: event sourcing over CQRS",
    Tokens: 15,
})

// When hot tier fills up, migrate
hotToWarm, warmToCold, coldToArchive := s.Migrate()

// Search archived context
results := s.Recall("event sourcing", "session-1", 5)
```

## License

Apache 2.0 — zero dependencies (stdlib only).
