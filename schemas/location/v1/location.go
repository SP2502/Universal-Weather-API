package v1

type Location struct {
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	FormattedAddress string  `json:"formatted_address,omitempty"`
	HouseNumber      string  `json:"house_number,omitempty"`
	Street           string  `json:"street,omitempty"`
	Neighborhood     string  `json:"neighborhood,omitempty"`
	City             string  `json:"city,omitempty"`
	District         string  `json:"district,omitempty"`
	State            string  `json:"state,omitempty"`
	Country          string  `json:"country,omitempty"`
	CountryCode      string  `json:"country_code,omitempty"`
	PostalCode       string  `json:"postal_code,omitempty"`
	Timezone         string  `json:"timezone,omitempty"`
	Precision        string  `json:"precision,omitempty"`   // rooftop, building, street, neighborhood, locality, administrative_area, country, unknown
	Confidence       float64 `json:"confidence,omitempty"`  // 0.0 to 1.0 score if available
	ResultType       string  `json:"result_type,omitempty"` // point_of_interest, address, locality, etc.
}

type GeocodeResponse struct {
	SchemaVersion string     `json:"schema_version"` // "location.v1"
	Results       []Location `json:"results"`
}
