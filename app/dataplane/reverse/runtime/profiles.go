package runtime

type OperationProfile struct {
	TimeoutS     float64
	MaxRetries   int
	RetryCodes   []int
	RetryDelayS  float64
	IdleTimeoutS float64
}

func DefaultOperationProfile() OperationProfile {
	return OperationProfile{
		TimeoutS:    30.0,
		RetryDelayS: 1.0,
	}
}

func (p OperationProfile) RetriesStatus(statusCode int) bool {
	for _, code := range p.RetryCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

var (
	ChatProfile = OperationProfile{
		TimeoutS:     120.0,
		MaxRetries:   1,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  2.0,
		IdleTimeoutS: 30.0,
	}
	ImageProfile = OperationProfile{
		TimeoutS:     300.0,
		RetryDelayS:  1.0,
		IdleTimeoutS: 60.0,
	}
	ImageEditProfile = OperationProfile{
		TimeoutS:     120.0,
		MaxRetries:   1,
		RetryCodes:   []int{502, 503},
		RetryDelayS:  2.0,
		IdleTimeoutS: 30.0,
	}
	VideoProfile = OperationProfile{
		TimeoutS:    60.0,
		MaxRetries:  1,
		RetryCodes:  []int{429, 502, 503},
		RetryDelayS: 5.0,
	}
	VoiceProfile = OperationProfile{
		TimeoutS:     120.0,
		RetryDelayS:  1.0,
		IdleTimeoutS: 15.0,
	}
	AssetProfile = OperationProfile{
		TimeoutS:    60.0,
		MaxRetries:  2,
		RetryCodes:  []int{502, 503},
		RetryDelayS: 1.0,
	}
	GRPCProfile = OperationProfile{
		TimeoutS:    15.0,
		MaxRetries:  1,
		RetryCodes:  []int{503},
		RetryDelayS: 0.5,
	}
)

var Profiles = map[string]OperationProfile{
	"chat":       ChatProfile,
	"image":      ImageProfile,
	"image_edit": ImageEditProfile,
	"video":      VideoProfile,
	"voice":      VoiceProfile,
	"asset":      AssetProfile,
	"grpc":       GRPCProfile,
}
