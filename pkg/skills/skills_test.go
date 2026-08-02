package skills

import "testing"

func newSeed() *Skill {
	return &Skill{
		ID:      "lark-im-reply@1.0.0",
		Name:    "lark-im-reply",
		Version: "1.0.0",
		Owner:   "cm-success",
		Summary: "reply to Feishu messages",
		Tags:    []string{"feishu", "im"},
	}
}

func TestAddSeed_PersonalStage(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	err := s.AddSeed(newSeed())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := s.Get("lark-im-reply@1.0.0")
	if got.Stage != StagePersonal {
		t.Errorf("stage = %s, want personal", got.Stage)
	}
}

func TestAddSeed_Duplicate(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	if err := s.AddSeed(newSeed()); err == nil {
		t.Error("expected error for duplicate skill ID")
	}
}

func TestCanTransition_Matrix(t *testing.T) {
	cases := []struct {
		from, to Stage
		want     bool
	}{
		{StagePersonal, StageShared, true},
		{StagePersonal, StagePromoted, false},
		{StagePersonal, StagePersonal, true}, // no-op
		{StageShared, StagePromoted, true},
		{StageShared, StagePersonal, true},
		{StagePromoted, StageOrgPack, true},
		{StagePromoted, StagePersonal, false},
		{StageOrgPack, StageArchived, true},
		{StageOrgPack, StagePromoted, false},
		{StageArchived, StagePersonal, true}, // recall
		{StageArchived, StagePromoted, false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestTransition_Invalid(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	if err := s.Transition("lark-im-reply@1.0.0", StagePromoted); err == nil {
		t.Error("expected error: personal -> promoted is invalid")
	}
	if err := s.Transition("no-such", StageShared); err == nil {
		t.Error("expected error: missing skill")
	}
}

func TestLifecycle_FullPath(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())

	// personal -> shared
	if err := s.Transition("lark-im-reply@1.0.0", StageShared); err != nil {
		t.Fatalf("shared transition: %v", err)
	}
	// grant to another agent
	if err := s.GrantShare("lark-im-reply@1.0.0", "cm-success", "cm-bridge"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !s.CanUse("lark-im-reply@1.0.0", "cm-bridge") {
		t.Error("grantee should be able to use shared skill")
	}
	if s.CanUse("lark-im-reply@1.0.0", "cm-deepsight") {
		t.Error("non-grantee should NOT use shared skill")
	}
	// owner always can
	if !s.CanUse("lark-im-reply@1.0.0", "cm-success") {
		t.Error("owner should always use own skill")
	}
}

func TestGrantShare_RequiresShared(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	// still personal — grant must fail
	if err := s.GrantShare("lark-im-reply@1.0.0", "cm-success", "cm-bridge"); err == nil {
		t.Error("expected error: grant before shared")
	}
}

func TestGrantShare_NonOwner(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	s.Transition("lark-im-reply@1.0.0", StageShared)
	if err := s.GrantShare("lark-im-reply@1.0.0", "cm-deepsight", "cm-bridge"); err == nil {
		t.Error("expected error: non-owner cannot grant")
	}
}

func TestRecordUse_SuccessRate(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	skill := s.Get("lark-im-reply@1.0.0")

	// 9 successes, 1 failure → rate 0.9
	for i := 0; i < 9; i++ {
		s.RecordUse(skill.ID, true)
	}
	s.RecordUse(skill.ID, false)

	if skill.UsageCount != 10 {
		t.Errorf("usage = %d, want 10", skill.UsageCount)
	}
	if skill.SuccessRate < 0.89 || skill.SuccessRate > 0.91 {
		t.Errorf("success rate = %.3f, want ~0.9", skill.SuccessRate)
	}
}

func TestPromotionEligible_Gates(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	s.Transition("lark-im-reply@1.0.0", StageShared)
	skill := s.Get("lark-im-reply@1.0.0")

	// Not eligible: no usage, no trust
	ok, failures := s.PromotionEligible(skill.ID)
	if ok {
		t.Error("should not be eligible with zero evidence")
	}
	if len(failures) < 3 {
		t.Errorf("expected multiple gate failures, got %v", failures)
	}

	// Meet gates (MinSharedDays bypassed via UpdatedAt manipulation)
	for i := 0; i < 10; i++ {
		s.RecordUse(skill.ID, true)
	}
	skill.TrustScore = 0.8
	skill.UpdatedAt = skill.UpdatedAt.AddDate(0, 0, -15) // 15 days ago

	ok, failures = s.PromotionEligible(skill.ID)
	if !ok {
		t.Errorf("should be eligible, failures: %v", failures)
	}
}

func TestPromotionEligible_NotShared(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	ok, _ := s.PromotionEligible("lark-im-reply@1.0.0")
	if ok {
		t.Error("personal skill cannot be promotion-eligible")
	}
}

func TestCanUse_PromotedEveryone(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	s.Transition("lark-im-reply@1.0.0", StageShared)
	s.Transition("lark-im-reply@1.0.0", StagePromoted)

	if !s.CanUse("lark-im-reply@1.0.0", "cm-deepsight") {
		t.Error("promoted skill should be usable by any org agent")
	}
}

func TestArchiveExpired(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	s.Transition("lark-im-reply@1.0.0", StageShared)

	// Force idle: set UpdatedAt to > MaxArchivedAge days ago
	skill := s.Get("lark-im-reply@1.0.0")
	skill.UpdatedAt = skill.UpdatedAt.AddDate(0, 0, -(DefaultPromotionPolicy().MaxArchivedAge + 1))

	n := s.ArchiveExpired()
	if n != 1 {
		t.Errorf("archived %d skills, want 1", n)
	}
	if skill.Stage != StageArchived {
		t.Errorf("stage = %s, want archived", skill.Stage)
	}
	// Archived is read-only
	if s.CanUse(skill.ID, "cm-success") {
		t.Error("archived skill should not be usable")
	}
}

func TestSearch(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	results := s.Search("feishu", 10)
	if len(results) != 1 {
		t.Errorf("search 'feishu' = %d results, want 1", len(results))
	}
	results = s.Search("nothing-matches", 10)
	if len(results) != 0 {
		t.Errorf("search 'nothing' = %d results, want 0", len(results))
	}
}

func TestStats(t *testing.T) {
	s := NewStore(DefaultPromotionPolicy())
	s.AddSeed(newSeed())
	stats := s.Stats()
	if stats[StagePersonal] != 1 {
		t.Errorf("personal count = %d, want 1", stats[StagePersonal])
	}
}
