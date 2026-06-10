package shared

type ModeID int

const (
	ModeAuto ModeID = iota
	ModeFast
	ModeExpert
	ModeHeavy
	ModeGrok43
	ModeConsole
)

type PoolID int

const (
	PoolBasic PoolID = iota
	PoolSuper
	PoolHeavy
)

type StatusID int

const (
	StatusActive StatusID = iota
	StatusCooling
	StatusExpired
	StatusDisabled
	StatusDeleted
)

var PoolStringToID = map[string]int{
	"basic": int(PoolBasic),
	"super": int(PoolSuper),
	"heavy": int(PoolHeavy),
}

var PoolIDToString = map[int]string{
	int(PoolBasic): "basic",
	int(PoolSuper): "super",
	int(PoolHeavy): "heavy",
}

var StatusStringToID = map[string]int{
	"active":   int(StatusActive),
	"cooling":  int(StatusCooling),
	"expired":  int(StatusExpired),
	"disabled": int(StatusDisabled),
}

var AllModeIDs = []int{
	int(ModeAuto),
	int(ModeFast),
	int(ModeExpert),
	int(ModeHeavy),
	int(ModeGrok43),
	int(ModeConsole),
}
