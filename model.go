package main

// Payload 是 /v2.5/<token>/{lng},{lat}/weather 的响应，只保留报告用到的字段
type Payload struct {
	Status     string `json:"status"`
	Msg        string `json:"msg"`
	ServerTime int64  `json:"server_time"`
	TZShift    int    `json:"tzshift"`
	Timezone   string `json:"timezone"`
	Result     Result `json:"result"`
}

type Result struct {
	Realtime         Realtime `json:"realtime"`
	Hourly           Hourly   `json:"hourly"`
	Daily            Daily    `json:"daily"`
	Alert            Alert    `json:"alert"`
	ForecastKeypoint string   `json:"forecast_keypoint"`
}

type Wind struct {
	Speed     float64 `json:"speed"`
	Direction float64 `json:"direction"`
}

type AirQuality struct {
	PM25        int               `json:"pm25"`
	AQI         map[string]int    `json:"aqi"`
	Description map[string]string `json:"description"`
}

type LifeNow struct {
	Index int    `json:"index"`
	Desc  string `json:"desc"`
}

type Realtime struct {
	Temperature         float64            `json:"temperature"`
	ApparentTemperature float64            `json:"apparent_temperature"`
	Humidity            float64            `json:"humidity"`
	Visibility          float64            `json:"visibility"`
	Pressure            float64            `json:"pressure"`
	Skycon              string             `json:"skycon"`
	Wind                Wind               `json:"wind"`
	Precipitation       PrecipitationNow   `json:"precipitation"`
	AirQuality          AirQuality         `json:"air_quality"`
	LifeIndex           map[string]LifeNow `json:"life_index"`
}

type PrecipitationNow struct {
	Local struct {
		Intensity float64 `json:"intensity"`
	} `json:"local"`
}

type ValueItem struct {
	Datetime string  `json:"datetime"`
	Value    float64 `json:"value"`
}

type StringItem struct {
	Datetime string `json:"datetime"`
	Value    string `json:"value"`
}

type WindItem struct {
	Datetime  string  `json:"datetime"`
	Speed     float64 `json:"speed"`
	Direction float64 `json:"direction"`
}

type Hourly struct {
	Temperature   []ValueItem  `json:"temperature"`
	Precipitation []ValueItem  `json:"precipitation"`
	Humidity      []ValueItem  `json:"humidity"`
	Cloudrate     []ValueItem  `json:"cloudrate"`
	Skycon        []StringItem `json:"skycon"`
	Wind          []WindItem   `json:"wind"`
}

type DailyTemp struct {
	Date string  `json:"date"`
	Max  float64 `json:"max"`
	Min  float64 `json:"min"`
	Avg  float64 `json:"avg"`
}

type DailyString struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type DailyWind struct {
	Date string `json:"date"`
	Avg  Wind   `json:"avg"`
}

type DailyAstro struct {
	Date    string `json:"date"`
	Sunrise struct {
		Time string `json:"time"`
	} `json:"sunrise"`
	Sunset struct {
		Time string `json:"time"`
	} `json:"sunset"`
}

type LifeItem struct {
	Date  string `json:"date"`
	Index string `json:"index"`
	Desc  string `json:"desc"`
}

type DailyLife struct {
	Ultraviolet []LifeItem `json:"ultraviolet"`
	CarWashing  []LifeItem `json:"carWashing"`
	Dressing    []LifeItem `json:"dressing"`
	Comfort     []LifeItem `json:"comfort"`
	ColdRisk    []LifeItem `json:"coldRisk"`
}

type Daily struct {
	Temperature   []DailyTemp   `json:"temperature"`
	Skycon        []DailyString `json:"skycon"`
	Precipitation []DailyTemp   `json:"precipitation"`
	Wind          []DailyWind   `json:"wind"`
	Astro         []DailyAstro  `json:"astro"`
	LifeIndex     DailyLife     `json:"life_index"`
}

type Alert struct {
	Content []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"content"`
}
