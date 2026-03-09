package icaoregion

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocator_RegionOfLatLon(t *testing.T) {
	testData := []struct {
		cityName string
		lat, lon float64
		expected Region
	}{
		// --- AFI (Africa–Indian Ocean) ---
		{"Abuja, Nigeria", 9.0765, 7.3986, AFI},
		{"Accra, Ghana", 5.6037, -0.1870, AFI},
		{"Nairobi, Kenya", -1.2921, 36.8219, AFI},
		{"Addis Ababa, Ethiopia", 8.9806, 38.7578, AFI},
		{"Pretoria, South Africa", -25.7479, 28.2293, AFI},

		// --- ASIA ---
		{"New Delhi, India", 28.6139, 77.2090, ASIA},
		{"Beijing, China", 39.9042, 116.4074, ASIA},
		{"Tokyo, Japan", 35.6895, 139.6917, ASIA},
		{"Bangkok, Thailand", 13.7563, 100.5018, ASIA},
		{"Jakarta, Indonesia", -6.2088, 106.8456, ASIA},
		{"Canberra, Australia", -35.2809, 149.1300, ASIA},
		{"Wellington, New Zealand", -41.2866, 174.7762, ASIA},

		// --- CAR (Caribbean) ---
		{"Havana, Cuba", 23.1136, -82.3666, CAR},
		{"Kingston, Jamaica", 17.9712, -76.7936, CAR},
		{"Port-au-Prince, Haiti", 18.5944, -72.3074, CAR},
		{"Santo Domingo, DR", 18.4861, -69.9312, CAR},
		{"Nassau, Bahamas", 25.0443, -77.3504, CAR},
		{"Mexico City, Mexico", 19.4326, -99.1332, CAR},

		// --- EUR (Europe) ---
		{"London, United Kingdom", 51.5074, -0.1278, EUR},
		{"Paris, France", 48.8566, 2.3522, EUR},
		{"Berlin, Germany", 52.5200, 13.4050, EUR},
		{"Madrid, Spain", 40.4168, -3.7038, EUR},
		{"Rome, Italy", 41.9028, 12.4964, EUR},
		{"Amsterdam, Netherlands", 52.3676, 4.9041, EUR},
		{"Vienna, Austria", 48.2082, 16.3738, EUR},

		// --- MID (Middle East) ---
		{"Riyadh, Saudi Arabia", 24.7136, 46.6753, MID},
		{"Abu Dhabi, UAE", 24.4539, 54.3773, MID},
		{"Doha, Qatar", 25.2854, 51.5310, MID},
		{"Kuwait City, Kuwait", 29.3759, 47.9774, MID},
		{"Amman, Jordan", 31.9539, 35.9106, MID},
		{"Cairo, Egypt", 30.0444, 31.2357, MID},

		// --- NAM (North America) ---
		{"Washington, DC, USA", 38.9072, -77.0369, NAM},
		{"Ottawa, Canada", 45.4215, -75.6972, NAM},

		// --- NAT (North Atlantic – oceanic airspace; no capitals) ---
		{"NAT Ocean Point 1", 50.0000, -30.0000, NAT},
		{"NAT Ocean Point 2", 45.0000, -20.0000, NAT},

		// --- SAM (South America) ---
		{"Brasília, Brazil", -15.7939, -47.8828, SAM},
		{"Buenos Aires, Argentina", -34.6037, -58.3816, SAM},
		{"Santiago, Chile", -33.4489, -70.6693, SAM},
		{"Lima, Peru", -12.0464, -77.0428, SAM},
		{"Quito, Ecuador", -0.1807, -78.4678, SAM},
		{"Bogotá, Colombia", 4.7110, -74.0721, SAM},
		{"Port of Spain, Trinidad", 10.6549, -61.5019, SAM},
	}

	L, err := NewLocator()
	require.NoError(t, err, "error creating NewLocator")

	for _, tc := range testData {
		t.Run(tc.cityName, func(t *testing.T) {
			r := L.RegionOfLatLon(tc.lat, tc.lon)
			assert.Equal(t, tc.expected, r, "expected %v, got %v", tc.expected, r)
		})
	}
}
