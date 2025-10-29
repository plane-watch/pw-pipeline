package feedercache

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"plane.watch/lib/export"
)

func makeTestData() (*FeederCache, uuid.UUID) {

	testFeederUUID := uuid.New()

	// populate testFeederProxy with some test data
	testFeederProxy := &FeederCache{}
	testFeederProxy.feeders = make(map[string]export.Feeder)
	testFeederProxy.feedersConnected = make(map[string]map[Protocol]struct{})
	testFeederProxy.feederConnectionTime = make(map[string]map[Protocol]time.Time)
	testFeederProxy.feeders[testFeederUUID.String()] = export.Feeder{
		MlatEnabled:   true,
		Id:            1,
		Latitude:      33.33333,
		Longitude:     -111.11111,
		Altitude:      0,
		User:          "Test User",
		FeedDirection: "0",
		FeedProtocol:  "0",
		Label:         "TestFeeder",
		Mux:           "mux-#wa",
		FeederCode:    "TEST-0001",
		ApiKey:        testFeederUUID,
	}

	return testFeederProxy, testFeederUUID
}

func TestFeederProxy_IsValid(t *testing.T) {
	testFeederProxy, testFeederUUID := makeTestData()
	assert.True(t, testFeederProxy.IsValid(testFeederUUID.String()))
	assert.False(t, testFeederProxy.IsValid(uuid.New().String()))
}

func TestFeederProxy_IsConnected(t *testing.T) {
	testFeederProxy, testFeederUUID := makeTestData()
	assert.False(t, testFeederProxy.IsConnected(testFeederUUID.String(), BEAST))
	testFeederProxy.SetConnected(testFeederUUID.String(), BEAST)
	assert.True(t, testFeederProxy.IsConnected(testFeederUUID.String(), BEAST))
	testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
	assert.False(t, testFeederProxy.IsConnected(testFeederUUID.String(), BEAST))
}

func TestFeederProxy_IsConnectingTooFrequently(t *testing.T) {
	testFeederProxy, testFeederUUID := makeTestData()
	synctest.Test(t, func(t *testing.T) {
		// ensure previous tests cleaned up
		testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
		time.Sleep(10 * time.Minute)
		testFeederProxy.cleanup()

		t.Cleanup(func() {
			// ensure this test cleaned up
			testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
			time.Sleep(10 * time.Minute)
			testFeederProxy.cleanup()
		})

		// connect & disconnect
		assert.False(t, testFeederProxy.IsConnectingTooFrequently(testFeederUUID.String(), BEAST))
		testFeederProxy.SetConnected(testFeederUUID.String(), BEAST)
		testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)

		// attempt fast reconnect, should be classed as too frequent
		assert.True(t, testFeederProxy.IsConnectingTooFrequently(testFeederUUID.String(), BEAST))

		// wait for 1 minute (using synctest so instant)
		time.Sleep(1 * time.Minute)

		// should now be able to connect
		assert.False(t, testFeederProxy.IsConnectingTooFrequently(testFeederUUID.String(), BEAST))

		// attempt fast reconnect again, should be classed as too frequent
		testFeederProxy.SetConnected(testFeederUUID.String(), BEAST)
		testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
		assert.True(t, testFeederProxy.IsConnectingTooFrequently(testFeederUUID.String(), BEAST))
	})
}

func TestFeederProxy_Authenticate(t *testing.T) {
	testFeederProxy, testFeederUUID := makeTestData()
	synctest.Test(t, func(t *testing.T) {
		// ensure previous tests cleaned up
		testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
		time.Sleep(10 * time.Minute)
		testFeederProxy.cleanup()

		t.Cleanup(func() {
			// ensure this test cleaned up
			testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
			time.Sleep(10 * time.Minute)
			testFeederProxy.cleanup()
		})

		// attempt to authenticate with known api key, should work
		a, err := testFeederProxy.Authenticate(testFeederUUID.String(), BEAST)
		assert.NoError(t, err)
		assert.True(t, a)

		// attempt to authenticate when feeder already connected, should fail
		testFeederProxy.SetConnected(testFeederUUID.String(), BEAST)
		a, err = testFeederProxy.Authenticate(testFeederUUID.String(), BEAST)
		assert.Error(t, err)
		assert.False(t, a)

		// attempt to authenticate when feeder too soon after previous connection, should fail
		testFeederProxy.SetDisconnected(testFeederUUID.String(), BEAST)
		a, err = testFeederProxy.Authenticate(testFeederUUID.String(), BEAST)
		assert.Error(t, err)
		assert.False(t, a)

		// wait for 1 minute (using synctest so instant)
		time.Sleep(1 * time.Minute)

		// attempt to authenticate with known api key, should work
		a, err = testFeederProxy.Authenticate(testFeederUUID.String(), BEAST)
		assert.NoError(t, err)
		assert.True(t, a)
	})
}
