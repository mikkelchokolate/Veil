package installer

type RURecommendedDefaults struct {
	Username      string
	MasqueradeURL string
	FallbackRoot  string
}

func NewRURecommendedDefaults() RURecommendedDefaults {
	return RURecommendedDefaults{
		Username:      "veil",
		MasqueradeURL: "https://www.bing.com/",
		FallbackRoot:  "/var/lib/veil/www",
	}
}
