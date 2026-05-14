package forgetfulmap

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

type testPlaneLocation struct {
	Icao  string
	Index int32
}

func setupNonSweepingForgetfulSyncMap(sweepInterval time.Duration, oldAfter time.Duration) (f ForgetfulSyncMap) {
	testMap := ForgetfulSyncMap{
		lookup:        &sync.Map{},
		sweepInterval: sweepInterval,
		oldAfter:      oldAfter,
		forgettable:   OldAfterForgettableAction(oldAfter),
	}

	return testMap
}

func TestForgetfulSyncMap_Len(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(10))

	planeOne := testPlaneLocation{
		Icao:  "VH67SH",
		Index: 1,
	}

	planeTwo := testPlaneLocation{
		Icao:  "JU7281",
		Index: 2,
	}

	planeThree := testPlaneLocation{
		Icao:  "YS8219",
		Index: 3,
	}

	testMap.Store(planeOne.Icao, planeOne)
	testMap.Store(planeTwo.Icao, planeTwo)
	testMap.Store(planeThree.Icao, planeThree)

	if testMap.Len() != 3 {
		t.Error("Len() has incorrect number of planes - 1")
	}
}

func TestForgetfulSyncMap_SweepOldPlane(t *testing.T) {
	testMap := setupNonSweepingForgetfulSyncMap(1*time.Second, 60*time.Second)

	planeOne := testPlaneLocation{
		Icao:  "VH67SH",
		Index: 1,
	}
	planeTwo := testPlaneLocation{
		Icao:  "VH666",
		Index: 2,
	}

	// store a test plane, 61 seconds ago.
	testMap.lookup.Store(planeOne.Icao, &marble{
		added: time.Now().Add(-61 * time.Second),
		value: planeOne,
	})
	testMap.Store(planeTwo.Icao, planeTwo) // normal item, will be wrapped in a marble struct

	if testMap.Len() != 2 {
		t.Error("Failed to correctly store our items in the map")
	}

	// sweep up the old plane
	testMap.sweep()

	if testMap.Len() != 1 {
		t.Error("Sweeper didn't sweep an old plane.")
	}

	if _, ok := testMap.Load(planeOne.Icao); ok {
		t.Error("Failed to remove our old item")
	}

	if _, ok := testMap.Load(planeTwo.Icao); !ok {
		t.Error("Accidentally removed the wrong item, expected planeTwo to still be there")
	}

}

func TestForgetfulSyncMap_DontSweepNewPlane(t *testing.T) {
	testMap := setupNonSweepingForgetfulSyncMap(1*time.Second, 60*time.Second)

	testPlane := testPlaneLocation{
		"VH57312",
		1,
	}

	testMap.Store(testPlane.Icao, testPlane)

	if testMap.Len() != 1 {
		t.Error("Test plane not added.")
	}

	// this shouldn't sweep our new plane.
	testMap.sweep()

	if testMap.Len() != 1 {
		t.Error("Test plane was incorrectly swept.")
	}
}

func TestForgetfulSyncMap_LoadFound(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))

	testPlane := testPlaneLocation{
		"VH7832AH",
		1,
	}

	testMap.Store(testPlane.Icao, testPlane)

	testLoadedPlane, ok := testMap.Load(testPlane.Icao)

	if ok {
		if testLoadedPlane != testPlane {
			t.Error("The loaded plane doesn't match the test plane.")
		}
	} else {
		t.Error("Load failed.")
	}
}

func TestForgetfulSyncMap_LoadNotFound(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))
	testVal, testBool := testMap.Load("VH123GH")
	if testVal != nil {
		t.Error("A not-found value didn't return nil")
	}
	if testBool != false {
		t.Error("Found boolean was incorrect.")
	}
}

func TestForgetfulSyncMap_AddKey(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))
	testKey := "VH123CH"
	testMap.AddKey(testKey)

	value, successBool := testMap.Load(testKey)

	if !successBool {
		t.Error("Test key was not found.")
	}

	if value != nil {
		t.Errorf("Something other (%+v) than a nil value was found.", value)
	}
}

func TestForgetfulSyncMap_HasKeyFound(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))
	testKey := "VH1234CT"

	testMap.AddKey(testKey)

	result := testMap.HasKey(testKey)

	if !result {
		t.Error("Key wasn't present when it should have been.")
	}
}

func TestForgetfulSyncMap_HasKeyNotFound(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))

	testMap.AddKey("VH1234CT")

	result := testMap.HasKey("NOTKEY")

	if result {
		t.Error("Key was present when it should not have been.")
	}
}

func TestForgetfulSyncMap_Delete(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))
	testKey := "VH123CG"

	testMap.AddKey(testKey)

	if !testMap.HasKey(testKey) {
		t.Error("Key doesn't exist.")
	}

	testMap.Delete(testKey)

	if testMap.HasKey(testKey) {
		t.Error("Key still exists after being deleted.")
	}
}

func TestForgetfulSyncMap_Range(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))

	type testItem struct {
		value string
	}

	item := testItem{value: "item 222"}
	testMap.Store("test", item)

	if testMap.Len() != 1 {
		t.Error("Failed to store test item")
	}

	// make sure we can get our item out
	loadedItem, found := testMap.Load("test")
	if !found || nil == loadedItem {
		t.Error("Failed load our item")
	}

	typedLoadedItem, tOk := loadedItem.(testItem)
	if !tOk {
		t.Error("Failed to get our test item out unmolested")
	}
	if typedLoadedItem.value != "item 222" {
		t.Errorf("item came out changed?! - %+v", item)
	}

	counter := 0
	testMap.Range(func(key, value interface{}) bool {
		counter++
		typedLoadedItem, tOk = loadedItem.(testItem)
		if !tOk {
			t.Error("Failed to get our test item out unmolested")
		}
		if typedLoadedItem.value != "item 222" {
			t.Errorf("item came out changed?! - %+v", item)
		}

		return true
	})

	if counter != 1 {
		t.Error("Failed to range correctly through the map")
	}
}

func TestForgetfulSyncMap_SweepWithCustomExpiryFunc(t *testing.T) {
	type testItem struct {
		value  string
		remove bool
	}
	testMap := NewForgetfulSyncMap(WithForgettableAction(func(key, value any, added time.Time) bool {
		v, ok := value.(testItem)
		if !ok {
			t.Error("Incorrect type returned, expected testItem{}, got", reflect.TypeOf(value))
			return true
		}
		return v.remove
	}))

	item1 := testItem{value: "item 111", remove: false}
	testMap.Store("item1", item1)
	item2 := testItem{value: "item 222", remove: true}
	testMap.Store("item2", item2)

	if testMap.Len() != 2 {
		t.Error("Failed to store test items")
	}

	testMap.sweep()
	if testMap.Len() != 1 {
		t.Error("Failed to remove our test2 item on sweep")
	}

	// make sure we can get our item out
	loadedItem, found := testMap.Load("item1")
	if !found || nil == loadedItem {
		t.Error("Failed load our item1")
	}
}

func TestForgetfulSyncMap_LoadOrStore(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second), WithOldAgeAfterSeconds(60))

	// First call should store and return the value
	val1, loaded := testMap.LoadOrStore("key1", "value1")
	if loaded {
		t.Error("Expected loaded=false for first LoadOrStore")
	}
	if val1 != "value1" {
		t.Errorf("Expected value1, got %v", val1)
	}

	// Second call with same key should return existing value
	val2, loaded := testMap.LoadOrStore("key1", "value2")
	if !loaded {
		t.Error("Expected loaded=true for second LoadOrStore")
	}
	if val2 != "value1" {
		t.Errorf("Expected value1 (original), got %v", val2)
	}

	// Verify Load returns the original value
	val3, ok := testMap.Load("key1")
	if !ok {
		t.Error("Expected key1 to exist")
	}
	if val3 != "value1" {
		t.Errorf("Expected value1 from Load, got %v", val3)
	}

	// Different key should store successfully
	val4, loaded := testMap.LoadOrStore("key2", "value2")
	if loaded {
		t.Error("Expected loaded=false for new key")
	}
	if val4 != "value2" {
		t.Errorf("Expected value2, got %v", val4)
	}

	if testMap.Len() != 2 {
		t.Errorf("Expected 2 items, got %d", testMap.Len())
	}
}

func TestForgetfulSyncMap_LoadOrStore_Concurrent(t *testing.T) {
	testMap := NewForgetfulSyncMap(WithSweepInterval(time.Second*60), WithOldAgeAfterSeconds(60))

	const goroutines = 100
	var wg sync.WaitGroup
	stored := make(chan string, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			val, loaded := testMap.LoadOrStore("race-key", id)
			if !loaded {
				stored <- "stored"
			}
			// All goroutines should see the same value (the first one stored)
			_ = val
		}(i)
	}
	wg.Wait()
	close(stored)

	storeCount := 0
	for range stored {
		storeCount++
	}
	if storeCount != 1 {
		t.Errorf("Expected exactly 1 goroutine to store, got %d", storeCount)
	}

	if testMap.Len() != 1 {
		t.Errorf("Expected 1 item, got %d", testMap.Len())
	}
}

func BenchmarkForgetfulSyncMap(b *testing.B) {
	m := NewForgetfulSyncMap(
		UseMemSyncPool(false),
		WithSweepInterval(time.Second*300),
		WithOldAgeAfterSeconds(300),
		WithForgettableAction(func(key, value any, added time.Time) bool {
			return false
		}),
	)

	var i, j int64
	for n := 0; n < b.N; n++ {
		for j = 0; j < 100; j++ {
			m.AddKey((i * 100) + j)
		}
		for j = 0; j < 100; j++ {
			m.Delete((i * 100) + j)
		}
	}
}

func BenchmarkForgetfulSyncMapPool(b *testing.B) {
	m := NewForgetfulSyncMap(
		UseMemSyncPool(true),
		WithSweepInterval(time.Second*300),
		WithOldAgeAfterSeconds(300),
		WithForgettableAction(func(key, value any, added time.Time) bool {
			return false
		}),
	)

	var i, j int64
	for n := 0; n < b.N; n++ {
		for j = 0; j < 100; j++ {
			m.AddKey((i * 100) + j)
		}
		for j = 0; j < 100; j++ {
			m.Delete((i * 100) + j)
		}
	}
}
