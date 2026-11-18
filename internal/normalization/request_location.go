package normalization

import (
	"errors"
	"fmt"
	loc "github.com/universal-api/platform/schemas/location/v1"
)

// ValidateRequestedLocation rejects grossly misrouted responses while allowing
// documented grid/city rounding. These ceilings do not certify vendor coverage.
func ValidateRequestedLocation(id string, latitude, longitude *float64, actual *loc.Location) error {
	if err := ValidateLocation(actual); err != nil {
		return err
	}
	if latitude == nil && longitude == nil {
		return nil
	} // unresolved vendor place query
	if latitude == nil || longitude == nil || !validGeo(*latitude, *longitude) {
		return errors.New("invalid requested coordinates")
	}
	var limit float64
	switch id {
	case "openweather", "weatherapi":
		limit = 50
	case "met_norway", "bright_sky":
		limit = 25
	case "open_meteo_forecast":
		limit = 50
	case "us_nws":
		limit = 25
	default:
		return errors.New("source has no reviewed location tolerance")
	}
	if distanceKM(*latitude, *longitude, actual.Latitude, actual.Longitude) > limit {
		return fmt.Errorf("source location exceeds %g km request tolerance", limit)
	}
	return nil
}
