#!/usr/bin/env python3
"""UWA canonical weather.v1, location.v1 and forecast.v1 client example.

Use UWA_API_KEY from the environment. No production credentials in source.
"""
import json
import os
import urllib.error
import urllib.parse
import urllib.request


class UniversalApiClient:
    def __init__(self, api_key: str, base_url: str = "http://127.0.0.1:8080"):
        self.api_key = api_key
        self.base_url = base_url.rstrip("/")

    def _get(self, path: str, **params):
        uri = self.base_url + path + "?" + urllib.parse.urlencode(params)
        req = urllib.request.Request(uri, headers={"X-API-Key": self.api_key, "Accept": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=15) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            # No upstream credentials are included in this error.
            raise RuntimeError(f"UWA HTTP {exc.code}") from exc

    def get_weather(self, location: str, provider=None):
        args = {"location": location}
        if provider:
            args["provider"] = provider
        return self._get("/v1/weather", **args)

    def get_forecast(self, lat: float, lon: float, provider=None):
        args = {"lat": lat, "lon": lon}
        if provider:
            args["provider"] = provider
        return self._get("/v1/forecast", **args)

    def geocode(self, query: str, limit: int = 10):
        return self._get("/v1/geocode", q=query, limit=limit)


if __name__ == "__main__":
    key = os.environ.get("UWA_API_KEY")
    if not key:
        raise SystemExit("Set UWA_API_KEY to a gateway client API key")
    client = UniversalApiClient(key)
    result = client.get_weather("London")
    weather = result["data"]
    print(f"Weather: {weather['location'].get('city', 'requested place')}; "
          f"{weather['temperature']['value']} {weather['temperature']['unit']}; "
          f"condition={weather['condition']}; provider={result['meta'].get('provider', 'unknown')}")
