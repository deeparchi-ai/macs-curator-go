// Package dfsms implements MACS §7: Knowledge lifecycle and memory
// compression for multi-agent sessions.
//
// Agent sessions accumulate context: conversation history, tool results,
// subagent outputs, retrieved documents. Context windows are finite
// (128K–1M tokens). Without lifecycle management, old context is either
// dropped blindly or kept entirely (cost explosion + attention dilution).
//
// z/OS DFSMS (1978) solved this for disk storage with tiered migration:
// high-speed → nearline → archive → tape. This package applies the same
// principle to agent context.
//
// Four tiers:
//   - Hot (0): last ~10 turns, full fidelity
//   - Warm (1): last 10–50 turns, summarized with key decisions
//   - Cold (2): session history, bullet-point summary
//   - Archive (3): cross-session, searchable index
package dfsms

import (
	"fmt"
	"time"
)

// Tier represents a storage tier in the DFSMS hierarchy.
type Tier int

const (
	TierHot     Tier = iota // full fidelity, ~50K tokens
	TierWarm                 // summarized, ~10K tokens
	TierCold                 // bullet-point, ~1K tokens
	TierArchive              // searchable index, unlimited
)

// String returns the tier name.
func (t Tier) String() string {
	switch t {
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	case TierCold:
		return "cold"
	case TierArchive:
		return "archive"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// ContextChunk represents a piece of agent context at a specific tier.
type ContextChunk struct {
	ID        string    // unique identifier
	SessionID string    // which session
	Tier      Tier      // current storage tier
	Content   string    // the actual context text
	Summary   string    // LLM-generated summary (warm tier)
	Keywords  []string  // extracted keywords (cold/archive tier)
	Tokens    int       // estimated token count
	CreatedAt time.Time
	AccessedAt time.Time
	Decisions []string // key decisions extracted from this chunk
}

// MigrationPolicy defines when context migrates between tiers.
type MigrationPolicy struct {
	HotToWarmTokens  int // e.g., 50000 → compress oldest hot to warm
	WarmToColdTokens int // e.g., 100000 → compress oldest warm to cold
	ColdTTL          time.Duration // e.g., 7 days → archive
	MaxHotChunks     int // max chunks in hot tier
	MaxWarmChunks    int // max chunks in warm tier
}

// DefaultPolicy returns a reasonable default migration policy.
func DefaultPolicy() MigrationPolicy {
	return MigrationPolicy{
		HotToWarmTokens:  50000,
		WarmToColdTokens: 100000,
		ColdTTL:          7 * 24 * time.Hour,
		MaxHotChunks:     20,
		MaxWarmChunks:    50,
	}
}

// Store manages the four-tier context lifecycle.
type Store struct {
	policy  MigrationPolicy
	chunks  map[string]*ContextChunk
	totalTokens int
}

// NewStore creates a DFSMS context store with the given policy.
func NewStore(policy MigrationPolicy) *Store {
	return &Store{
		policy: policy,
		chunks: make(map[string]*ContextChunk),
	}
}

// Add inserts a new context chunk into the hot tier.
func (s *Store) Add(chunk *ContextChunk) {
	chunk.Tier = TierHot
	chunk.CreatedAt = time.Now()
	chunk.AccessedAt = time.Now()
	s.chunks[chunk.ID] = chunk
	s.totalTokens += chunk.Tokens
}

// Get retrieves a chunk by ID and updates its access time.
func (s *Store) Get(id string) *ContextChunk {
	chunk, ok := s.chunks[id]
	if !ok {
		return nil
	}
	chunk.AccessedAt = time.Now()
	return chunk
}

// Migrate runs one migration cycle, moving context between tiers
// based on the configured policy. Returns counts per migration type.
func (s *Store) Migrate() (hotToWarm, warmToCold, coldToArchive int) {
	// Hot → Warm: when total hot tokens exceed threshold
	hotChunks := s.chunksInTier(TierHot)
	if s.tokensInTier(TierHot) > s.policy.HotToWarmTokens || len(hotChunks) > s.policy.MaxHotChunks {
		// Move oldest hot chunks to warm
		oldest := s.oldestChunks(hotChunks, len(hotChunks)/4)
		for _, c := range oldest {
			c.Tier = TierWarm
			c.Summary = s.summarize(c.Content)
			hotToWarm++
		}
	}

	// Warm → Cold: when total warm tokens exceed threshold
	warmChunks := s.chunksInTier(TierWarm)
	if s.tokensInTier(TierWarm) > s.policy.WarmToColdTokens || len(warmChunks) > s.policy.MaxWarmChunks {
		oldest := s.oldestChunks(warmChunks, len(warmChunks)/3)
		for _, c := range oldest {
			c.Tier = TierCold
			c.Keywords = s.extractKeywords(c.Summary)
			warmToCold++
		}
	}

	// Cold → Archive: TTL expiration
	coldChunks := s.chunksInTier(TierCold)
	now := time.Now()
	for _, c := range coldChunks {
		if now.Sub(c.AccessedAt) > s.policy.ColdTTL {
			c.Tier = TierArchive
			coldToArchive++
		}
	}

	return
}

// Recall searches the archive tier for chunks matching a query.
// Returns ranked results (most recently accessed first).
func (s *Store) Recall(query string, sessionID string, limit int) []*ContextChunk {
	var results []*ContextChunk
	for _, c := range s.chunks {
		if c.Tier != TierArchive && c.Tier != TierCold {
			continue
		}
		if sessionID != "" && c.SessionID != sessionID {
			continue
		}
		// Simple keyword match
		if s.matchesQuery(c, query) {
			results = append(results, c)
		}
	}
	// Sort by most recently accessed
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].AccessedAt.After(results[i].AccessedAt) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

// Promote moves a chunk back to the hot tier (e.g., on recall).
func (s *Store) Promote(id string) bool {
	chunk, ok := s.chunks[id]
	if !ok {
		return false
	}
	chunk.Tier = TierHot
	chunk.AccessedAt = time.Now()
	return true
}

// Stats returns tier-level statistics.
func (s *Store) Stats() map[Tier]int {
	stats := make(map[Tier]int)
	for _, c := range s.chunks {
		stats[c.Tier]++
	}
	return stats
}

// chunksInTier returns chunks at a specific tier.
func (s *Store) chunksInTier(tier Tier) []*ContextChunk {
	var result []*ContextChunk
	for _, c := range s.chunks {
		if c.Tier == tier {
			result = append(result, c)
		}
	}
	return result
}

// tokensInTier returns total tokens at a specific tier.
func (s *Store) tokensInTier(tier Tier) int {
	total := 0
	for _, c := range s.chunks {
		if c.Tier == tier {
			total += c.Tokens
		}
	}
	return total
}

// oldestChunks returns the N oldest chunks from a slice.
func (s *Store) oldestChunks(chunks []*ContextChunk, n int) []*ContextChunk {
	if n > len(chunks) {
		n = len(chunks)
	}
	// Sort by CreatedAt ascending
	sorted := make([]*ContextChunk, len(chunks))
	copy(sorted, chunks)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].CreatedAt.Before(sorted[i].CreatedAt) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return sorted[:n]
}

// summarize generates a placeholder summary. In production, this would
// call an LLM to extract key decisions.
func (s *Store) summarize(content string) string {
	if len(content) > 200 {
		return content[:200] + "..."
	}
	return content
}

// extractKeywords extracts keywords. In production, this would use
// embedding-based indexing.
func (s *Store) extractKeywords(text string) []string {
	// Placeholder: split and take first few words
	words := splitWords(text)
	if len(words) > 5 {
		words = words[:5]
	}
	return words
}

// matchesQuery checks if a chunk matches a recall query.
func (s *Store) matchesQuery(chunk *ContextChunk, query string) bool {
	if contains(chunk.Content, query) {
		return true
	}
	if contains(chunk.Summary, query) {
		return true
	}
	for _, kw := range chunk.Keywords {
		if contains(kw, query) {
			return true
		}
	}
	return false
}

func splitWords(s string) []string {
	var words []string
	current := ""
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' {
			if current != "" {
				words = append(words, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
