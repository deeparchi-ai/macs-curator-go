# macs-dfsms-go

MACS §7: Tiered knowledge lifecycle and memory compression for agent sessions.

**Status:** v0.2 — 27 tests (dfsms 13 + skills 14)

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

// Promote archived context to hot tier on demand
s.Promote("ctx-1")
```

## Skill seeding

The `pkg/skills` package extends Curator to manage *skills* (executable
knowledge): personal → shared → promoted → org-pack lifecycle, adapted from
QM's skill governance with agent-native grants and policy gates.

```go
import "github.com/deeparchi-ai/macs-dfsms-go/pkg/skills"

st := skills.NewStore(skills.DefaultPromotionPolicy())
st.AddSeed(&skills.Skill{ID: "lark-im-reply@1.0.0", Name: "lark-im-reply",
    Version: "1.0.0", Owner: "cm-success"})
st.Transition("lark-im-reply@1.0.0", skills.StageShared)
st.GrantShare("lark-im-reply@1.0.0", "cm-success", "cm-bridge")

ok, failures := st.PromotionEligible("lark-im-reply@1.0.0")
```

Design spec: `macs/specs/curator-skill-seeding.md`.

## License

Apache 2.0 — zero dependencies (stdlib only).
