package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	column "github.com/weaming/printable/go-column"
)

var skyconText = map[string]string{
	"CLEAR_DAY": "晴", "CLEAR_NIGHT": "晴夜",
	"PARTLY_CLOUDY_DAY": "多云", "PARTLY_CLOUDY_NIGHT": "多云夜",
	"CLOUDY":     "阴",
	"LIGHT_HAZE": "轻度雾霾", "MODERATE_HAZE": "中度雾霾", "HEAVY_HAZE": "重度雾霾",
	"LIGHT_RAIN": "小雨", "MODERATE_RAIN": "中雨", "HEAVY_RAIN": "大雨", "STORM_RAIN": "暴雨",
	"FOG":           "雾",
	"LIGHT_SNOW":    "小雪",
	"MODERATE_SNOW": "中雪",
	"HEAVY_SNOW":    "大雪",
	"STORM_SNOW":    "暴雪",
	"DUST":          "浮尘",
	"SAND":          "沙尘",
	"WIND":          "大风",
}

// lifeOrder / lifeLabels 固定 realtime.life_index 的输出顺序与列名
var (
	lifeNowOrder  = []string{"ultraviolet", "comfort"}
	lifeNowLabels = map[string]string{"ultraviolet": "紫外线", "comfort": "舒适"}
	lifeDayOrder  = []string{"ultraviolet", "carWashing", "dressing", "comfort", "coldRisk"}
	lifeDayLabels = map[string]string{
		"ultraviolet": "紫外线",
		"carWashing":  "洗车",
		"dressing":    "穿衣",
		"comfort":     "舒适",
		"coldRisk":    "感冒",
	}
	directions = []string{"北", "东北", "东", "东南", "南", "西南", "西", "西北"}
)

func skyText(skycon string) string {
	if text, ok := skyconText[skycon]; ok {
		return text
	}
	return skycon
}

func direction(degrees float64) string {
	index := int(degrees/45+0.5) % 8
	if index < 0 {
		index += 8
	}
	return directions[index]
}

func weekday(date string) string {
	parsed, err := time.Parse("2006-01-02T15:04-07:00", date)
	if err != nil {
		return ""
	}
	names := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return names[int(parsed.Weekday())]
}

// renderBlock 用 printable（util-linux column）把行对齐；cells 为空的行自动补齐
func renderBlock(rows [][]string) (string, error) {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}

	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, width)
		copy(cells, row)
		lines = append(lines, strings.Join(cells, "\t"))
	}

	separator := "\t"
	outputSeparator := "  "
	out, err := column.Render([]byte(strings.Join(lines, "\n")), []column.ColumnOption{
		{Name: "table"},
		{Name: "separator", Value: &separator},
		{Name: "output-separator", Value: &outputSeparator},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// RenderReport 生成与 python 版 caiyun 一致的报告：当前 + 逐小时 + 逐日 + 生活指数
func RenderReport(payload *Payload, name string, lng, lat float64, hours, days int) (string, error) {
	result := payload.Result
	real := result.Realtime
	stamp := time.Unix(payload.ServerTime+int64(payload.TZShift), 0).UTC().Format("01-02 15:04")

	now := [][]string{
		{"位置: " + orUnknown(name), fmt.Sprintf("%s,%s", formatCoord(lng), formatCoord(lat))},
		{"时间: " + stamp, "时区: " + payload.Timezone},
		{"实况: " + skyText(real.Skycon), fmt.Sprintf("温度: %.1f°C", real.Temperature)},
		{fmt.Sprintf("体感: %.1f°C", real.ApparentTemperature), fmt.Sprintf("湿度: %.0f%%", real.Humidity*100)},
		{fmt.Sprintf("能见度: %gkm", real.Visibility), fmt.Sprintf("气压: %.1fhPa", real.Pressure/100)},
		{fmt.Sprintf("风: %gm/s %s", real.Wind.Speed, direction(real.Wind.Direction)), fmt.Sprintf("降水: %gmm/h", real.Precipitation.Local.Intensity)},
	}
	if real.AirQuality.AQI != nil {
		now = append(now, []string{
			fmt.Sprintf("空气: AQI %d %s", real.AirQuality.AQI["chn"], real.AirQuality.Description["chn"]),
			fmt.Sprintf("PM2.5: %dμg/m³", real.AirQuality.PM25),
		})
	}
	var lifeCells []string
	for _, key := range lifeNowOrder {
		if item, ok := real.LifeIndex[key]; ok {
			lifeCells = append(lifeCells, fmt.Sprintf("%s: %s", lifeNowLabels[key], item.Desc))
		}
	}
	if len(lifeCells) > 0 {
		now = append(now, lifeCells)
	}
	for _, alert := range result.Alert.Content {
		now = append(now, []string{"预警: " + alert.Title, alert.Description})
	}
	if result.ForecastKeypoint != "" {
		now = append(now, []string{"", "短时: " + result.ForecastKeypoint})
	}

	var builder strings.Builder
	block, err := renderBlock(now)
	if err != nil {
		return "", err
	}
	builder.WriteString(block)

	hourRows := [][]string{{"时间", "天气", "温度", "降水", "湿度", "风"}}
	for index := 0; index < min(hours, len(result.Hourly.Temperature)); index++ {
		hourRows = append(hourRows, []string{
			formatHour(result.Hourly.Temperature[index].Datetime),
			skyText(result.Hourly.Skycon[index].Value),
			fmt.Sprintf("%.1f°C", result.Hourly.Temperature[index].Value),
			fmt.Sprintf("%g", result.Hourly.Precipitation[index].Value),
			fmt.Sprintf("%.0f%%", result.Hourly.Humidity[index].Value*100),
			fmt.Sprintf("%gm/s", result.Hourly.Wind[index].Speed),
		})
	}
	builder.WriteString("\n\n" + fmt.Sprintf("逐小时 · 未来 %dh", hours) + "\n")
	if block, err = renderBlock(hourRows); err != nil {
		return "", err
	}
	builder.WriteString(block)

	dayRows := [][]string{{"日期", "星期", "天气", "最低", "最高", "降水", "日出", "日落"}}
	dayCount := min(days, len(result.Daily.Temperature))
	for index := 0; index < dayCount; index++ {
		temp := result.Daily.Temperature[index]
		dayRows = append(dayRows, []string{
			temp.Date[:10],
			weekday(temp.Date),
			skyText(result.Daily.Skycon[index].Value),
			fmt.Sprintf("%.1f°C", temp.Min),
			fmt.Sprintf("%.1f°C", temp.Max),
			fmt.Sprintf("%g", result.Daily.Precipitation[index].Avg),
			result.Daily.Astro[index].Sunrise.Time,
			result.Daily.Astro[index].Sunset.Time,
		})
	}
	builder.WriteString("\n\n" + fmt.Sprintf("逐日 · 未来 %dd", dayCount) + "\n")
	if block, err = renderBlock(dayRows); err != nil {
		return "", err
	}
	builder.WriteString(block)

	lifeRows := [][]string{{"日期", "紫外线", "洗车", "穿衣", "舒适", "感冒"}}
	for index := 0; index < min(days, len(result.Daily.LifeIndex.Ultraviolet)); index++ {
		row := []string{result.Daily.LifeIndex.Ultraviolet[index].Date[:10]}
		for _, key := range lifeDayOrder {
			row = append(row, dailyLife(lifeDayItems(result.Daily.LifeIndex, key), index))
		}
		lifeRows = append(lifeRows, row)
	}
	builder.WriteString("\n\n" + fmt.Sprintf("生活指数 · %dd", min(days, 16)) + "\n")
	if block, err = renderBlock(lifeRows); err != nil {
		return "", err
	}
	builder.WriteString(block)
	builder.WriteString("\n")

	return builder.String(), nil
}

func lifeDayItems(life DailyLife, key string) []LifeItem {
	switch key {
	case "ultraviolet":
		return life.Ultraviolet
	case "carWashing":
		return life.CarWashing
	case "dressing":
		return life.Dressing
	case "comfort":
		return life.Comfort
	case "coldRisk":
		return life.ColdRisk
	}
	return nil
}

func dailyLife(items []LifeItem, index int) string {
	if index < len(items) {
		return items[index].Desc
	}
	return ""
}

func formatHour(datetime string) string {
	parsed, err := time.Parse("2006-01-02T15:04-07:00", datetime)
	if err != nil {
		return datetime
	}
	return parsed.Format("01-02 15:04")
}

// formatCoord 网格坐标是 k/100，用最短表示即可（114.49 而不是 114.49000000000001）
func formatCoord(value float64) string {
	return strconv.FormatFloat(snapCoord(value), 'f', -1, 64)
}

func orUnknown(name string) string {
	if name == "" {
		return "未知位置"
	}
	return name
}
