package mlatbridge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"plane.watch/lib/icaoregion"
)

func TestMLATRegionByFID_NoDupes(t *testing.T) {
	tmp := make(map[int]int, 0)
	for _, fids := range MLATRegionByFID {
		for _, fid := range fids {
			if _, ok := tmp[fid]; !ok {
				tmp[fid] = 0
			}
			tmp[fid]++
		}
	}
	for fid, count := range tmp {
		assert.Lessf(t, count, 2, "fid %d has count of %d", fid, count)
	}
}

func TestMLATServer(t *testing.T) {
	testData := []struct {
		cityName string
		lat, lon float64
		expected string
	}{
		// Oceania
		{"Jakarta, Indonesia", -6.2088, 106.8456, MLATServers[Oceania]},
		{"Canberra, Australia", -35.2809, 149.1300, MLATServers[Oceania]},
		{"Wellington, New Zealand", -41.2866, 174.7762, MLATServers[Oceania]},
		{"Kuala Lumpur, Malaysia", 3.1390, 101.6869, MLATServers[Oceania]},
		{"Singapore, Singapore", 1.3521, 103.8198, MLATServers[Oceania]},
		{"Jakarta, Indonesia", -6.2088, 106.8456, MLATServers[Oceania]},
		{"Manila, Philippines", 14.5995, 120.9842, MLATServers[Oceania]},
		//
		// Asia
		{"New Delhi, India", 28.6139, 77.2090, MLATServers[Asia]},
		{"Beijing, China", 39.9042, 116.4074, MLATServers[Asia]},
		{"Tokyo, Japan", 35.6895, 139.6917, MLATServers[Asia]},
		{"Bangkok, Thailand", 13.7563, 100.5018, MLATServers[Asia]},
		{"New Delhi, India", 28.6139, 77.2090, MLATServers[Asia]},
		{"Mumbai, India", 19.0760, 72.8777, MLATServers[Asia]},
		{"Bangalore, India", 12.9716, 77.5946, MLATServers[Asia]},
		{"Chennai, India", 13.0827, 80.2707, MLATServers[Asia]},
		{"Kolkata, India", 22.5726, 88.3639, MLATServers[Asia]},
		{"Islamabad, Pakistan", 33.6844, 73.0479, MLATServers[Asia]},
		{"Karachi, Pakistan", 24.8607, 67.0011, MLATServers[Asia]},
		{"Lahore, Pakistan", 31.5204, 74.3587, MLATServers[Asia]},
		{"Moscow, Russia", 55.7558, 37.6173, MLATServers[Asia]},
		{"Novosibirsk, Russia", 55.0084, 82.9357, MLATServers[Asia]},
		{"Vladivostok, Russia", 43.1155, 131.8855, MLATServers[Asia]},
		{"Bangkok, Thailand", 13.7563, 100.5018, MLATServers[Asia]},
		{"Hanoi, Vietnam", 21.0278, 105.8342, MLATServers[Asia]},
		{"Ho Chi Minh City, Vietnam", 10.8231, 106.6297, MLATServers[Asia]},
		{"Phnom Penh, Cambodia", 11.5564, 104.9282, MLATServers[Asia]},
		{"Yangon, Myanmar", 16.8409, 96.1735, MLATServers[Asia]},
		//
		// Canada & Alaska
		{"Anchorage, USA", 61.2181, -149.9003, MLATServers[CA_Alaska]},
		{"Fairbanks, USA", 64.8378, -147.7164, MLATServers[CA_Alaska]},
		{"Vancouver, Canada", 49.2827, -123.1207, MLATServers[CA_Alaska]},
		{"Calgary, Canada", 51.0447, -114.0719, MLATServers[CA_Alaska]},
		{"Toronto, Canada", 43.6511, -79.3839, MLATServers[CA_Alaska]},
		{"Montreal, Canada", 45.5019, -73.5674, MLATServers[CA_Alaska]},
		//
		// EU West
		{"London, England", 51.5072, -0.1276, MLATServers[EU_West]},
		{"Manchester, England", 53.4808, -2.2426, MLATServers[EU_West]},
		{"Birmingham, England", 52.4862, -1.8904, MLATServers[EU_West]},
		{"Edinburgh, Scotland", 55.9533, -3.1883, MLATServers[EU_West]},
		{"Glasgow, Scotland", 55.8642, -4.2518, MLATServers[EU_West]},
		{"Dublin, Ireland", 53.3498, -6.2603, MLATServers[EU_West]},
		{"Cork, Ireland", 51.8985, -8.4756, MLATServers[EU_West]},
		{"Paris, France", 48.8566, 2.3522, MLATServers[EU_West]},
		{"Madrid, Spain", 40.4168, -3.7038, MLATServers[EU_West]},
		{"Barcelona, Spain", 41.3851, 2.1734, MLATServers[EU_West]},
		{"Seville, Spain", 37.3891, -5.9845, MLATServers[EU_West]},
		//
		// EU Central
		{"Rome, Italy", 41.9028, 12.4964, MLATServers[EU_Central]},
		{"Milan, Italy", 45.4642, 9.1900, MLATServers[EU_Central]},
		{"Florence, Italy", 43.7696, 11.2558, MLATServers[EU_Central]},
		{"Venice, Italy", 45.4408, 12.3155, MLATServers[EU_Central]},
		{"Berlin, Germany", 52.5200, 13.4050, MLATServers[EU_Central]},
		{"Munich, Germany", 48.1351, 11.5820, MLATServers[EU_Central]},
		{"Vienna, Austria", 48.2082, 16.3738, MLATServers[EU_Central]},
		{"Salzburg, Austria", 47.8095, 13.0550, MLATServers[EU_Central]},
		{"Prague, Czech Republic", 50.0755, 14.4378, MLATServers[EU_Central]},
		{"Warsaw, Poland", 52.2297, 21.0122, MLATServers[EU_Central]},
		{"Krakow, Poland", 50.0647, 19.9450, MLATServers[EU_Central]},
		{"Budapest, Hungary", 47.4979, 19.0402, MLATServers[EU_Central]},
		{"Bratislava, Slovakia", 48.1486, 17.1077, MLATServers[EU_Central]},
		{"Bucharest, Romania", 44.4268, 26.1025, MLATServers[EU_Central]},
		{"Zagreb, Croatia", 45.8150, 15.9819, MLATServers[EU_Central]},
		{"Belgrade, Serbia", 44.7866, 20.4489, MLATServers[EU_Central]},
		{"Sofia, Bulgaria", 42.6977, 23.3219, MLATServers[EU_Central]},
		{"Saint Petersburg, Russia", 59.9343, 30.3351, MLATServers[EU_Central]},
		//
		// US West
		{"Mexico City, Mexico", 19.4326, -99.1332, MLATServers[US_West]},
		{"Seattle, USA", 47.6062, -122.3321, MLATServers[US_West]},
		{"San Francisco, USA", 37.7749, -122.4194, MLATServers[US_West]},
		{"Los Angeles, USA", 34.0522, -118.2437, MLATServers[US_West]},
		//
		// US East
		{"New York City, USA", 40.7128, -74.0060, MLATServers[US_East]},
		{"Washington, D.C., USA", 38.9072, -77.0369, MLATServers[US_East]},
		{"Miami, USA", 25.7617, -80.1918, MLATServers[US_East]},
		//
		// South America
		{"Havana, Cuba", 23.1136, -82.3666, MLATServers[South_America]},
		{"Kingston, Jamaica", 17.9712, -76.7936, MLATServers[South_America]},
		{"Port-au-Prince, Haiti", 18.5944, -72.3074, MLATServers[South_America]},
		{"Santo Domingo, DR", 18.4861, -69.9312, MLATServers[South_America]},
		{"Nassau, Bahamas", 25.0443, -77.3504, MLATServers[South_America]},
		{"Tristan da Cunha", -37.1052, -12.2777, MLATServers[South_America]},
		//
		// Other
		{"Pretoria, South Africa", -25.7479, 28.2293, MLATServers[Unknown]},
		{"Accra, Ghana", 5.6037, -0.1870, MLATServers[Unknown]},
		{"Nairobi, Kenya", -1.2921, 36.8219, MLATServers[Unknown]},
		{"Addis Ababa, Ethiopia", 8.9806, 38.7578, MLATServers[Unknown]},
	}

	L, err := icaoregion.NewLocator()
	require.NoError(t, err, "error creating icaoregion.Locator")

	for _, tc := range testData {
		t.Run(tc.cityName, func(t *testing.T) {
			fid := L.FIDOfLatLon(tc.lat, tc.lon)
			assert.Equal(t, tc.expected, MLATServer(fid))
		})
	}
}
