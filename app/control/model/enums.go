package model

// ModeID is the upstream modeId value used on hot paths.
type ModeID int

const (
	ModeAuto ModeID = iota
	ModeFast
	ModeExpert
	ModeHeavy
	ModeGrok43
	ModeConsole
)

// ToAPIString returns the upstream API mode string.
func (m ModeID) ToAPIString() string {
	if m == ModeGrok43 {
		return "grok-420-computer-use-sa"
	}
	if value, ok := ModeStrings[m]; ok {
		return value
	}
	switch m {
	case ModeConsole:
		return "console"
	default:
		return ""
	}
}

// Tier determines which account pool is selected.
type Tier int

const (
	TierBasic Tier = iota
	TierSuper
	TierHeavy
)

// Capability is a bitmask of features a model supports.
type Capability int

const (
	CapabilityChat Capability = 1 << iota
	CapabilityImage
	CapabilityImageEdit
	CapabilityVideo
	CapabilityVoice
	CapabilityAsset
	CapabilityConsoleChat
)

// ModeStrings are human-readable mode strings in API order.
var ModeStrings = map[ModeID]string{
	ModeAuto:   "auto",
	ModeFast:   "fast",
	ModeExpert: "expert",
	ModeHeavy:  "heavy",
}

var (
	AllModes          = []ModeID{ModeAuto, ModeFast, ModeExpert}
	AllModesWithHeavy = []ModeID{ModeAuto, ModeFast, ModeExpert, ModeHeavy}
	AllModesFull      = []ModeID{ModeAuto, ModeFast, ModeExpert, ModeHeavy, ModeGrok43}
)
