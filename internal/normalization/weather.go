package normalization

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/provider"
	locv1 "github.com/universal-api/platform/schemas/location/v1"
	weatherv1 "github.com/universal-api/platform/schemas/weather/v1"
)

type WeatherNormalizer struct{}

func NewWeatherNormalizer() *WeatherNormalizer {
	return &WeatherNormalizer{}
}

func (n *WeatherNormalizer) Normalize(raw *provider.RawWeatherResponse) (*weatherv1.WeatherResponse, error) {
	if raw == nil {
		return nil, errors.New("raw response is nil")
	}

	switch raw.ProviderID {
	case "openweather":
		return n.normalizeOpenWeather(raw)
	case "weatherapi":
		return n.normalizeWeatherAPI(raw)
	default:
		return nil, fmt.Errorf("unsupported provider for normalization: %s", raw.ProviderID)
	}
}

func (n *WeatherNormalizer) normalizeOpenWeather(raw *provider.RawWeatherResponse) (*weatherv1.WeatherResponse, error) {
	// Re-marshal to JSON then decode if needed, or cast if already struct
	var rawBytes []byte
	var err error

	switch v := raw.RawPayload.(type) {
	case []byte:
		rawBytes = v
	default:
		rawBytes, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to encode raw payload: %w", err)
		}
	}
	if len(raw.RawJSON) > 0 {
		rawBytes = raw.RawJSON
	}

	type OWPayload struct {
		Coord *struct {
			Lon *float64 `json:"lon"`
			Lat *float64 `json:"lat"`
		} `json:"coord"`
		Weather []struct {
			ID          int    `json:"id"`
			Main        string `json:"main"`
			Description string `json:"description"`
		} `json:"weather"`
		Main *struct {
			Temp     *float64 `json:"temp"` // Kelvin; pointer preserves required field presence
			Humidity *int     `json:"humidity"`
		} `json:"main"`
		Name string `json:"name"`
		Dt   *int64 `json:"dt"`
	}

	var ow OWPayload
	if err := json.Unmarshal(rawBytes, &ow); err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}

	if ow.Coord == nil || ow.Coord.Lat == nil || ow.Coord.Lon == nil ||
		ow.Main == nil || ow.Main.Temp == nil || ow.Dt == nil || *ow.Dt <= 0 {
		return nil, fmt.Errorf("%w: required OpenWeather coordinates, temperature or observation time missing", provider.ErrMalformedResponse)
	}

	// Temperature: Kelvin -> Celsius (C = K - 273.15)
	tempC := *ow.Main.Temp - 273.15
	tempC = math.Round(tempC*100) / 100 // Round to 2 decimal places

	// Condition mapping
	var cond weatherv1.Condition = weatherv1.ConditionUnknown
	var source *weatherv1.SourceCondition
	if len(ow.Weather) > 0 {
		item := ow.Weather[0]
		cond = mapOpenWeatherCondition(item.ID, item.Main)
		source = &weatherv1.SourceCondition{Provider: "openweather", Code: item.ID, Text: item.Main, Description: item.Description}
	}

	// Never represent response retrieval time as the provider's observation time.
	observedAt := time.Unix(*ow.Dt, 0).UTC()

	var humidity *weatherv1.Humidity
	if ow.Main.Humidity != nil {
		humidity = &weatherv1.Humidity{
			Value: *ow.Main.Humidity,
			Unit:  "percent",
		}
	}

	resp := &weatherv1.WeatherResponse{
		SchemaVersion: "weather.v1",
		Location: locv1.Location{
			Latitude:  *ow.Coord.Lat,
			Longitude: *ow.Coord.Lon,
			City:      ow.Name,
		},
		Temperature: weatherv1.Temperature{
			Value: tempC,
			Unit:  "celsius",
		},
		Humidity:        humidity,
		Condition:       cond,
		SourceCondition: source,
		ObservedAt:      observedAt,
	}

	if err := ValidateWeather(resp); err != nil {
		return nil, fmt.Errorf("%w: normalized weather validation failed: %v", provider.ErrMalformedResponse, err)
	}

	return resp, nil
}

func (n *WeatherNormalizer) normalizeWeatherAPI(raw *provider.RawWeatherResponse) (*weatherv1.WeatherResponse, error) {
	var rawBytes []byte
	var err error

	switch v := raw.RawPayload.(type) {
	case []byte:
		rawBytes = v
	default:
		rawBytes, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to encode raw payload: %w", err)
		}
	}
	if len(raw.RawJSON) > 0 {
		rawBytes = raw.RawJSON
	}

	type WAPIPayload struct {
		Location *struct {
			Name    string   `json:"name"`
			Country string   `json:"country"`
			Lat     *float64 `json:"lat"`
			Lon     *float64 `json:"lon"`
			TzID    string   `json:"tz_id"`
		} `json:"location"`
		Current *struct {
			TempC     *float64 `json:"temp_c"`
			Humidity  *int     `json:"humidity"`
			Condition *struct {
				Text string `json:"text"`
				Code int    `json:"code"`
			} `json:"condition"`
			LastUpdatedEpoch *int64 `json:"last_updated_epoch"`
		} `json:"current"`
	}

	var wapi WAPIPayload
	if err := json.Unmarshal(rawBytes, &wapi); err != nil {
		return nil, fmt.Errorf("%w: %v", provider.ErrMalformedResponse, err)
	}

	if wapi.Location == nil || wapi.Location.Lat == nil || wapi.Location.Lon == nil ||
		wapi.Current == nil || wapi.Current.TempC == nil ||
		wapi.Current.LastUpdatedEpoch == nil || *wapi.Current.LastUpdatedEpoch <= 0 {
		return nil, fmt.Errorf("%w: required WeatherAPI coordinates, temperature or observation time missing", provider.ErrMalformedResponse)
	}

	tempC := math.Round(*wapi.Current.TempC*100) / 100

	cond := weatherv1.ConditionUnknown
	var source *weatherv1.SourceCondition
	if wapi.Current.Condition != nil {
		item := wapi.Current.Condition
		cond = mapWeatherAPICondition(item.Code, item.Text)
		source = &weatherv1.SourceCondition{Provider: "weatherapi", Code: item.Code, Text: item.Text}
	}

	observedAt := time.Unix(*wapi.Current.LastUpdatedEpoch, 0).UTC()

	var humidity *weatherv1.Humidity
	if wapi.Current.Humidity != nil {
		humidity = &weatherv1.Humidity{
			Value: *wapi.Current.Humidity,
			Unit:  "percent",
		}
	}

	resp := &weatherv1.WeatherResponse{
		SchemaVersion: "weather.v1",
		Location: locv1.Location{
			Latitude:  *wapi.Location.Lat,
			Longitude: *wapi.Location.Lon,
			City:      wapi.Location.Name,
			Country:   wapi.Location.Country,
			Timezone:  wapi.Location.TzID,
		},
		Temperature: weatherv1.Temperature{
			Value: tempC,
			Unit:  "celsius",
		},
		Humidity:        humidity,
		Condition:       cond,
		SourceCondition: source,
		ObservedAt:      observedAt,
	}

	if err := ValidateWeather(resp); err != nil {
		return nil, fmt.Errorf("%w: normalized weather validation failed: %v", provider.ErrMalformedResponse, err)
	}

	return resp, nil
}

func mapOpenWeatherCondition(id int, main string) weatherv1.Condition {
	switch {
	case id >= 200 && id < 300:
		return weatherv1.ConditionThunderstorm
	case id >= 300 && id < 600:
		return weatherv1.ConditionRain
	case id >= 600 && id < 700:
		return weatherv1.ConditionSnow
	case id == 701 || id == 741:
		return weatherv1.ConditionFog
	case id >= 700 && id < 800:
		// Smoke, dust, volcanic ash, squall and tornado are NOT fog.
		return weatherv1.ConditionUnknown
	case id == 800:
		return weatherv1.ConditionClear
	case id == 801 || id == 802:
		return weatherv1.ConditionPartlyCloudy
	case id == 803 || id == 804:
		return weatherv1.ConditionCloudy
	default:
		switch strings.ToLower(main) {
		case "clear":
			return weatherv1.ConditionClear
		case "clouds":
			return weatherv1.ConditionCloudy
		case "rain":
			return weatherv1.ConditionRain
		default:
			return weatherv1.ConditionUnknown
		}
	}
}

// WeatherAPI publishes numbered condition codes. Use codes as the authority:
// localized condition text is preserved in SourceCondition, never guessed into
// a canonical class. Mixed sleet/ice-pellet categories have no exact weather.v1
// equivalent and are represented as unknown rather than a false snow/rain type.
func mapWeatherAPICondition(code int, _ string) weatherv1.Condition {
	switch code {
	case 1000:
		return weatherv1.ConditionClear
	case 1003:
		return weatherv1.ConditionPartlyCloudy
	case 1006, 1009:
		return weatherv1.ConditionCloudy
	case 1030, 1135, 1147:
		return weatherv1.ConditionFog
	case 1063, 1072, 1150, 1153, 1168, 1171, 1180, 1183, 1186, 1189,
		1192, 1195, 1198, 1201, 1240, 1243, 1246:
		return weatherv1.ConditionRain
	case 1066, 1114, 1117, 1210, 1213, 1216, 1219, 1222, 1225,
		1255, 1258:
		return weatherv1.ConditionSnow
	case 1087, 1273, 1276, 1279, 1282:
		return weatherv1.ConditionThunderstorm
	// Sleet and ice-pellet codes 1069, 1204, 1207, 1237, 1249,
	// 1252, 1261 and 1264 cannot be expressed precisely in weather.v1.
	default:
		return weatherv1.ConditionUnknown
	}
}
