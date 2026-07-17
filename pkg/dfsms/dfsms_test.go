package dfsms

import (
	"fmt"
	"testing"
	"time"
)

func TestTier_String(t *testing.T) {
	if TierHot.String() != "hot" {
		t.Errorf("TierHot = %q", TierHot.String())
	}
	if TierArchive.String() != "archive" {
		t.Errorf("TierArchive = %q", TierArchive.String())
	}
}

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.HotToWarmTokens != 50000 {
		t.Errorf("HotToWarmTokens = %d, want 50000", p.HotToWarmTokens)
	}
	if p.MaxHotChunks != 20 {
		t.Errorf("MaxHotChunks = %d", p.MaxHotChunks)
	}
}

func TestAddAndGet(t *testing.T) {
	s := NewStore(DefaultPolicy())
	chunk := &ContextChunk{
		ID:        "ctx-1",
		SessionID: "session-a",
		Content:   "The architecture uses microservices with event sourcing.",
		Tokens:    15,
	}
	s.Add(chunk)

	got := s.Get("ctx-1")
	if got == nil {
		t.Fatal("chunk should exist")
	}
	if got.Tier != TierHot {
		t.Errorf("new chunks start at hot tier, got %s", got.Tier)
	}
}

func TestGetMissing(t *testing.T) {
	s := NewStore(DefaultPolicy())
	if got := s.Get("nonexistent"); got != nil {
		t.Error("expected nil for missing chunk")
	}
}

func TestMigrate_HotToWarm(t *testing.T) {
	p := DefaultPolicy()
	p.HotToWarmTokens = 100 // low threshold to trigger migration
	s := NewStore(p)

	// Add chunks exceeding the hot token threshold
	for i := 0; i < 10; i++ {
		s.Add(&ContextChunk{
			ID:        fmt.Sprintf("hot-%d", i),
			Content:   fmt.Sprintf("Context chunk number %d with enough tokens to exceed threshold", i),
			Tokens:    20,
		})
	}

	hotToWarm, _, _ := s.Migrate()
	if hotToWarm == 0 {
		t.Error("expected hot→warm migration when hot tokens exceed threshold")
	}

	stats := s.Stats()
	if stats[TierWarm] == 0 {
		t.Error("warm tier should have chunks after migration")
	}
}

func TestMigrate_WarmToCold(t *testing.T) {
	p := DefaultPolicy()
	p.HotToWarmTokens = 50
	p.WarmToColdTokens = 100
	s := NewStore(p)

	// Fill hot tier
	for i := 0; i < 20; i++ {
		s.Add(&ContextChunk{
			ID:      fmt.Sprintf("chunk-%d", i),
			Content: fmt.Sprintf("Long content for chunk %d that will be migrated through tiers", i),
			Tokens:  20,
		})
	}

	// Migrate hot → warm
	s.Migrate()
	// Migrate again with low warm threshold
	s.policy.WarmToColdTokens = 50
	_, warmToCold, _ := s.Migrate()

	if warmToCold == 0 {
		t.Error("expected warm→cold migration")
	}
}

func TestMigrate_ColdToArchive(t *testing.T) {
	p := DefaultPolicy()
	p.ColdTTL = 1 * time.Nanosecond // immediate expiration
	s := NewStore(p)

	chunk := &ContextChunk{
		ID:        "old-chunk",
		Content:   "expired content",
		Tokens:    5,
		Tier:      TierCold,
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	s.Add(chunk)
	chunk.Tier = TierCold // override: add puts in hot

	_, _, coldToArchive := s.Migrate()
	if coldToArchive == 0 {
		t.Error("expected cold→archive migration for expired chunk")
	}
}

func TestRecall(t *testing.T) {
	s := NewStore(DefaultPolicy())

	// Add chunks directly to archive (bypass Add which forces Tier=Hot)
	s.chunks["arc-1"] = &ContextChunk{
		ID:        "arc-1",
		SessionID: "s1",
		Content:   "RACF resource profiles define access control",
		Keywords:  []string{"RACF", "access", "control"},
		Tier:      TierArchive,
	}
	s.chunks["arc-2"] = &ContextChunk{
		ID:        "arc-2",
		SessionID: "s1",
		Content:   "WLM schedules by goal-oriented importance",
		Keywords:  []string{"WLM", "scheduling"},
		Tier:      TierArchive,
	}

	results := s.Recall("RACF", "s1", 5)
	if len(results) == 0 {
		t.Error("expected results for RACF query")
	}
	if len(results) > 0 && results[0].ID != "arc-1" {
		t.Errorf("expected arc-1 for RACF query, got %s", results[0].ID)
	}
}

func TestRecall_FiltersBySession(t *testing.T) {
	s := NewStore(DefaultPolicy())

	s.chunks["cs-1"] = &ContextChunk{
		ID: "cs-1", SessionID: "s1", Content: "data for s1",
		Keywords: []string{"data"}, Tier: TierArchive,
	}
	s.chunks["cs-2"] = &ContextChunk{
		ID: "cs-2", SessionID: "s2", Content: "data for s2",
		Keywords: []string{"data"}, Tier: TierArchive,
	}

	results := s.Recall("data", "s1", 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for s1, got %d", len(results))
	}
	if results[0].SessionID != "s1" {
		t.Errorf("expected s1, got %s", results[0].SessionID)
	}
}

func TestPromote(t *testing.T) {
	s := NewStore(DefaultPolicy())
	s.Add(&ContextChunk{
		ID: "promo-1", Content: "important context",
		Tier: TierCold,
	})

	if !s.Promote("promo-1") {
		t.Error("promote should return true")
	}
	chunk := s.Get("promo-1")
	if chunk.Tier != TierHot {
		t.Errorf("promoted chunk should be hot, got %s", chunk.Tier)
	}
}

func TestPromote_Missing(t *testing.T) {
	s := NewStore(DefaultPolicy())
	if s.Promote("nonexistent") {
		t.Error("promote missing should return false")
	}
}

func TestStats(t *testing.T) {
	s := NewStore(DefaultPolicy())
	s.Add(&ContextChunk{ID: "a", Tokens: 10})
	s.Add(&ContextChunk{ID: "b", Tokens: 10})

	stats := s.Stats()
	if stats[TierHot] != 2 {
		t.Errorf("expected 2 hot chunks, got %d", stats[TierHot])
	}
}

func TestRecall_Limit(t *testing.T) {
	s := NewStore(DefaultPolicy())
	for i := 0; i < 10; i++ {
		s.chunks[fmt.Sprintf("r%d", i)] = &ContextChunk{
			ID: fmt.Sprintf("r%d", i), Content: "match",
			Keywords: []string{"match"}, Tier: TierArchive,
		}
	}
	results := s.Recall("match", "", 3)
	if len(results) != 3 {
		t.Errorf("limit=3, got %d", len(results))
	}
}
