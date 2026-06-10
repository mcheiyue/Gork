package model

// ModelSpec is the single source of truth for one model variant.
type ModelSpec struct {
	ModelName  string
	ModeID     ModeID
	Tier       Tier
	Capability Capability
	Enabled    bool
	PublicName string
	PreferBest bool
}

func (s ModelSpec) IsChat() bool {
	return s.Capability&CapabilityChat != 0
}

func (s ModelSpec) IsImage() bool {
	return s.Capability&CapabilityImage != 0
}

func (s ModelSpec) IsImageEdit() bool {
	return s.Capability&CapabilityImageEdit != 0
}

func (s ModelSpec) IsVideo() bool {
	return s.Capability&CapabilityVideo != 0
}

func (s ModelSpec) IsVoice() bool {
	return s.Capability&CapabilityVoice != 0
}

func (s ModelSpec) IsConsoleChat() bool {
	return s.Capability&CapabilityConsoleChat != 0
}

func (s ModelSpec) PoolName() string {
	if s.Tier == TierSuper {
		return "super"
	}
	if s.Tier == TierHeavy {
		return "heavy"
	}
	return "basic"
}

func (s ModelSpec) PoolID() int {
	return int(s.Tier)
}

func (s ModelSpec) PoolCandidates() []int {
	if s.PreferBest {
		if s.Tier == TierHeavy {
			return []int{2}
		}
		if s.Tier == TierSuper {
			return []int{2, 1}
		}
		return []int{2, 1, 0}
	}
	if s.Tier == TierBasic {
		return []int{0, 1, 2}
	}
	if s.Tier == TierSuper {
		return []int{1, 2}
	}
	return []int{2}
}
