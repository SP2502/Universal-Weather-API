/** UWA canonical contracts. No hard-coded API keys or assumed missing values. */
export interface Location {
  latitude: number;
  longitude: number;
  city?: string;
  country?: string;
  country_code?: string;
  formatted_address?: string;
  [field: string]: unknown;
}
export interface Temperature { value: number; unit: "celsius" }
export interface Humidity { value: number; unit: "percent" }
export type Condition = "clear" | "partly_cloudy" | "cloudy" | "rain" | "snow" | "thunderstorm" | "fog" | "unknown";
export interface WeatherData {
  schema_version: "weather.v1";
  location: Location;
  temperature: Temperature;
  humidity?: Humidity;
  condition: Condition;
  source_condition?: { provider: string; code: number; text?: string; description?: string };
  observed_at: string;
}
export interface ForecastPoint { valid_at: string; temperature: Temperature; humidity?: Humidity; condition: Condition; source_condition?: string }
export interface ForecastData { schema_version: "forecast.v1"; data_kind: "model_forecast"; location: Location; issued_at?: string; points: ForecastPoint[] }
export interface GeocodeData { schema_version: "location.v1"; results: Location[] }
export interface Meta {
  request_id: string; timestamp: string; provider?: string; cached: boolean; fallback: boolean;
  sources?: string[]; attribution?: { provider: string; text: string; url?: string }[]; warnings?: string[];
}
export interface ApiResponse<T> { data: T; meta: Meta }
export class UniversalApiClient {
  constructor(private apiKey: string, private baseUrl = "http://127.0.0.1:8080") {}
  private async get<T>(route: string, params: Record<string,string>): Promise<ApiResponse<T>> {
    const url = new URL(route, this.baseUrl);
    Object.entries(params).forEach(([k,v]) => url.searchParams.set(k,v));
    const response = await fetch(url.toString(),{headers:{"X-API-Key":this.apiKey,"Accept":"application/json"}});
    if(!response.ok) throw new Error(`UWA HTTP ${response.status}`);
    return await response.json() as ApiResponse<T>;
  }
  getWeather(location: string, provider?: string) { return this.get<WeatherData>("/v1/weather", {...(provider?{provider}:{}),location}); }
  getForecast(lat: number, lon: number, provider?: string) { return this.get<ForecastData>("/v1/forecast", {...(provider?{provider}:{}),lat:String(lat),lon:String(lon)}); }
  geocode(q: string, limit = 10) { return this.get<GeocodeData>("/v1/geocode",{q,limit:String(limit)}); }
}
