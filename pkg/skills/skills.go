// Package skills implements MACS §7 skill seeding: the lifecycle of
// agent-executable skills from personal creation to org-pack promotion.
//
// The model is adapted from QM's skill governance (scope-owned → share by
// grant → admin-gated promotion → git-based skill packs) with one key
// change: grants and promotion are decided by policy + telemetry + trust
// scoring (agent-native), not by human org hierarchy.
//
// z/OS lineage: SMP/E — features move local → test → production through
// validation gates before reaching production LPARs. Same here:
// personal → shared → promoted → org-pack, each stage with gates.
package skills

import (
	"fmt"
	"time"
)

// Stage is the lifecycle stage of a skill.
type Stage int

const (
	// StagePersonal — owner-only, zero friction to create.
	StagePersonal Stage = iota
	// StageShared — visible to grantees (Sanctum-authorized grants).
	StageShared
	// StagePromoted — visible to all agents in the org.
	StagePromoted
	// StageOrgPack — versioned, git-based distribution.
	StageOrgPack
	// StageArchived — read-only index, recall allowed.
	StageArchived
)

// String returns the stage name.
func (s Stage) String() string {
	switch s {
	case StagePersonal:
		return "personal"
	case StageShared:
		return "shared"
	case StagePromoted:
		return "promoted"
	case StageOrgPack:
		return "org-pack"
	case StageArchived:
		return "archived"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// Skill is an agent-executable procedure.
type Skill struct {
	ID          string    // stable identifier (name@version)
	Name        string    // e.g. "lark-im-reply"
	Version     string    // semver
	Owner       string    // agent LU name
	Stage       Stage     // current lifecycle stage
	Summary     string    // what it does (for Curator recall)
	UsageCount  int       // invocations (Gauge source)
	SuccessRate float64   // 0..1 (Gauge source)
	TrustScore  float64   // 0..1 (Sanctum source)
	Tags        []string  // for recall/search
	Body        string    // skill content (SKILL.md or reference)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Grant is an agent-to-agent skill grant.
type Grant struct {
	SkillID   string
	Grantor   string // owner LU
	Grantee   string // receiving agent LU
	GrantedAt time.Time
	RevokedAt *time.Time
}

// validTransitions defines which stage transitions are allowed.
var validTransitions = map[Stage]map[Stage]bool{
	StagePersonal: {StageShared: true, StageArchived: true},
	StageShared:   {StagePromoted: true, StagePersonal: true, StageArchived: true},
	StagePromoted: {StageOrgPack: true, StageArchived: true},
	StageOrgPack:  {StageArchived: true},
	StageArchived: {StagePersonal: true}, // recall → re-seed
}

// CanTransition reports whether moving from stage a to b is valid.
func CanTransition(a, b Stage) bool {
	if a == b {
		return true // no-op
	}
	return validTransitions[a][b]
}

// PromotionPolicy defines when a shared skill may be promoted.
type PromotionPolicy struct {
	MinUsageCount  int     // e.g. 10 invocations
	MinSuccessRate float64 // e.g. 0.9
	MinTrustScore  float64 // e.g. 0.7 (Sanctum)
	MinSharedDays  int     // e.g. 14 days in shared
	MaxArchivedAge int     // days before auto-archive without use
}

// DefaultPromotionPolicy returns sensible defaults.
func DefaultPromotionPolicy() PromotionPolicy {
	return PromotionPolicy{
		MinUsageCount:  10,
		MinSuccessRate: 0.9,
		MinTrustScore:  0.7,
		MinSharedDays:  14,
		MaxArchivedAge: 90,
	}
}

// Store manages skill lifecycle.
type Store struct {
	policy PromotionPolicy
	skills map[string]*Skill
	grants []Grant
}

// NewStore creates a skill store with the given policy.
func NewStore(policy PromotionPolicy) *Store {
	return &Store{
		policy: policy,
		skills: make(map[string]*Skill),
	}
}

// AddSeed creates a new skill in personal stage. Returns an error if the
// skill ID already exists.
func (s *Store) AddSeed(skill *Skill) error {
	if skill == nil || skill.ID == "" {
		return fmt.Errorf("skills: skill ID required")
	}
	if _, exists := s.skills[skill.ID]; exists {
		return fmt.Errorf("skills: skill %q already exists", skill.ID)
	}
	skill.Stage = StagePersonal
	skill.CreatedAt = time.Now()
	skill.UpdatedAt = time.Now()
	s.skills[skill.ID] = skill
	return nil
}

// Get returns a skill by ID.
func (s *Store) Get(id string) *Skill {
	return s.skills[id]
}

// Transition moves a skill to a new stage if the transition is valid.
// Returns an error for invalid transitions or missing skills.
func (s *Store) Transition(id string, to Stage) error {
	skill, ok := s.skills[id]
	if !ok {
		return fmt.Errorf("skills: no such skill %q", id)
	}
	if !CanTransition(skill.Stage, to) {
		return fmt.Errorf("skills: invalid transition %s -> %s", skill.Stage, to)
	}
	skill.Stage = to
	skill.UpdatedAt = time.Now()
	return nil
}

// GrantShare authorizes grantee to use a shared skill. Returns an error if
// the skill is not owned by grantor.
func (s *Store) GrantShare(skillID, grantor, grantee string) error {
	skill, ok := s.skills[skillID]
	if !ok {
		return fmt.Errorf("skills: no such skill %q", skillID)
	}
	if skill.Owner != grantor {
		return fmt.Errorf("skills: %q is not owner of %q", grantor, skillID)
	}
	// Grant requires the skill to be at least shared.
	if skill.Stage == StagePersonal {
		return fmt.Errorf("skills: %q must be shared before granting", skillID)
	}
	now := time.Now()
	s.grants = append(s.grants, Grant{
		SkillID:   skillID,
		Grantor:   grantor,
		Grantee:   grantee,
		GrantedAt: now,
	})
	return nil
}

// CanUse reports whether an agent may use a skill.
func (s *Store) CanUse(skillID, agent string) bool {
	skill, ok := s.skills[skillID]
	if !ok {
		return false
	}
	switch skill.Stage {
	case StagePersonal:
		return skill.Owner == agent
	case StageShared, StagePromoted, StageOrgPack:
		if skill.Owner == agent {
			return true
		}
		if skill.Stage == StageShared {
			return s.hasGrant(skillID, agent)
		}
		return true // promoted/org-pack: all org agents
	case StageArchived:
		return false // read-only
	default:
		return false
	}
}

// RecordUse updates usage telemetry for a skill (Gauge source).
func (s *Store) RecordUse(skillID string, success bool) {
	if skill, ok := s.skills[skillID]; ok {
		skill.UsageCount++
		if success {
			skill.SuccessRate = (skill.SuccessRate*float64(skill.UsageCount-1) + 1) / float64(skill.UsageCount)
		} else {
			skill.SuccessRate = (skill.SuccessRate * float64(skill.UsageCount-1)) / float64(skill.UsageCount)
		}
		skill.UpdatedAt = time.Now()
	}
}

// PromotionEligible evaluates whether a shared skill passes all gates.
func (s *Store) PromotionEligible(id string) (bool, []string) {
	skill, ok := s.skills[id]
	if !ok {
		return false, []string{"no such skill"}
	}
	if skill.Stage != StageShared {
		return false, []string{"not in shared stage"}
	}
	var failures []string
	if skill.UsageCount < s.policy.MinUsageCount {
		failures = append(failures, fmt.Sprintf("usage %d < min %d", skill.UsageCount, s.policy.MinUsageCount))
	}
	if skill.SuccessRate < s.policy.MinSuccessRate {
		failures = append(failures, fmt.Sprintf("success %.2f < min %.2f", skill.SuccessRate, s.policy.MinSuccessRate))
	}
	if skill.TrustScore < s.policy.MinTrustScore {
		failures = append(failures, fmt.Sprintf("trust %.2f < min %.2f", skill.TrustScore, s.policy.MinTrustScore))
	}
	// Time in shared is approximated by time since UpdatedAt.
	if daysInShared := int(time.Since(skill.UpdatedAt).Hours() / 24); daysInShared < s.policy.MinSharedDays {
		failures = append(failures, fmt.Sprintf("shared %dd < min %dd", daysInShared, s.policy.MinSharedDays))
	}
	return len(failures) == 0, failures
}

// ArchiveExpired moves skills with no use for MaxArchivedAge days to archive.
// Returns the number of skills archived.
func (s *Store) ArchiveExpired() int {
	archived := 0
	for _, skill := range s.skills {
		if skill.Stage == StageArchived || skill.Stage == StagePersonal {
			continue
		}
		if daysIdle := int(time.Since(skill.UpdatedAt).Hours() / 24); daysIdle > s.policy.MaxArchivedAge {
			skill.Stage = StageArchived
			skill.UpdatedAt = time.Now()
			archived++
		}
	}
	return archived
}

// Search finds skills matching a tag or name substring (Curator recall).
func (s *Store) Search(query string, limit int) []*Skill {
	var results []*Skill
	for _, skill := range s.skills {
		if matches(skill, query) {
			results = append(results, skill)
		}
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

// Stats returns stage-level statistics.
func (s *Store) Stats() map[Stage]int {
	stats := make(map[Stage]int)
	for _, skill := range s.skills {
		stats[skill.Stage]++
	}
	return stats
}

func (s *Store) hasGrant(skillID, agent string) bool {
	for _, g := range s.grants {
		if g.SkillID == skillID && g.Grantee == agent && g.RevokedAt == nil {
			return true
		}
	}
	return false
}

func matches(skill *Skill, query string) bool {
	if contains(skill.Name, query) || contains(skill.Summary, query) {
		return true
	}
	for _, t := range skill.Tags {
		if contains(t, query) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && search(s, substr)
}

func search(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
